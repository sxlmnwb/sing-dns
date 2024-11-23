package dns

import (
	"context"
	"crypto/tls"
	"net/netip"
	"net/url"
	"os"
	"sync"

	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/x/list"

	"github.com/miekg/dns"
)

var _ Upstream = (*TLSUpstream)(nil)

func init() {
	RegisterUpstream([]string{"tls"}, func(options UpstreamOptions) (Upstream, error) {
		return NewTLSUpstream(options)
	})
}

type TLSUpstream struct {
	dialer      N.Dialer
	serverAddr  M.Socksaddr
	access      sync.Mutex
	connections list.List[*tlsDNSConn]
}

type tlsDNSConn struct {
	*tls.Conn
	queryId uint16
}

func NewTLSUpstream(options UpstreamOptions) (*TLSUpstream, error) {
	serverURL, err := url.Parse(options.Address)
	if err != nil {
		return nil, err
	}
	serverAddr := M.ParseSocksaddr(serverURL.Host)
	if !serverAddr.IsValid() {
		return nil, E.New("invalid server address")
	}
	if serverAddr.Port == 0 {
		serverAddr.Port = 853
	}
	return newTLSUpstream(options, serverAddr), nil
}

func newTLSUpstream(options UpstreamOptions, serverAddr M.Socksaddr) *TLSUpstream {
	return &TLSUpstream{
		dialer:     options.Dialer,
		serverAddr: serverAddr,
	}
}

func (t *TLSUpstream) Start() error {
	return nil
}

func (t *TLSUpstream) Reset() {
	t.access.Lock()
	defer t.access.Unlock()
	for connection := t.connections.Front(); connection != nil; connection = connection.Next() {
		connection.Value.Close()
	}
	t.connections.Init()
}

func (t *TLSUpstream) Close() error {
	t.Reset()
	return nil
}

func (t *TLSUpstream) Raw() bool {
	return true
}

func (t *TLSUpstream) Exchange(ctx context.Context, message *dns.Msg) (*dns.Msg, error) {
	t.access.Lock()
	conn := t.connections.PopFront()
	t.access.Unlock()
	if conn != nil {
		response, err := t.exchange(message, conn)
		if err == nil {
			return response, nil
		}
	}
	tcpConn, err := t.dialer.DialContext(ctx, N.NetworkTCP, t.serverAddr)
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Client(tcpConn, &tls.Config{
		ServerName: t.serverAddr.AddrString(),
	})
	err = tlsConn.HandshakeContext(ctx)
	if err != nil {
		tcpConn.Close()
		return nil, err
	}
	return t.exchange(message, &tlsDNSConn{Conn: tlsConn})
}

func (t *TLSUpstream) exchange(message *dns.Msg, conn *tlsDNSConn) (*dns.Msg, error) {
	conn.queryId++
	err := writeMessage(conn, conn.queryId, message)
	if err != nil {
		conn.Close()
		return nil, E.Cause(err, "write request")
	}
	response, err := readMessage(conn)
	if err != nil {
		conn.Close()
		return nil, E.Cause(err, "read response")
	}
	t.access.Lock()
	t.connections.PushBack(conn)
	t.access.Unlock()
	return response, nil
}

func (t *TLSUpstream) Lookup(ctx context.Context, domain string, strategy DomainStrategy) ([]netip.Addr, error) {
	return nil, os.ErrInvalid
}
