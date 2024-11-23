package dns

import (
	"context"
	"encoding/binary"
	"io"
	"net/netip"
	"net/url"
	"os"

	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/miekg/dns"
)

var _ Upstream = (*TCPUpstream)(nil)

func init() {
	RegisterUpstream([]string{"tcp"}, func(options UpstreamOptions) (Upstream, error) {
		return NewTCPUpstream(options)
	})
}

type TCPUpstream struct {
	dialer     N.Dialer
	serverAddr M.Socksaddr
}

func NewTCPUpstream(options UpstreamOptions) (*TCPUpstream, error) {
	serverURL, err := url.Parse(options.Address)
	if err != nil {
		return nil, err
	}
	serverAddr := M.ParseSocksaddr(serverURL.Host)
	if !serverAddr.IsValid() {
		return nil, E.New("invalid server address")
	}
	if serverAddr.Port == 0 {
		serverAddr.Port = 53
	}
	return newTCPUpstream(options, serverAddr), nil
}

func newTCPUpstream(options UpstreamOptions, serverAddr M.Socksaddr) *TCPUpstream {
	return &TCPUpstream{
		dialer:     options.Dialer,
		serverAddr: serverAddr,
	}
}

func (t *TCPUpstream) Start() error {
	return nil
}

func (t *TCPUpstream) Reset() {
}

func (t *TCPUpstream) Close() error {
	return nil
}

func (t *TCPUpstream) Raw() bool {
	return true
}

func (t *TCPUpstream) Exchange(ctx context.Context, message *dns.Msg) (*dns.Msg, error) {
	conn, err := t.dialer.DialContext(ctx, N.NetworkTCP, t.serverAddr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	err = writeMessage(conn, 0, message)
	if err != nil {
		return nil, err
	}
	return readMessage(conn)
}

func (t *TCPUpstream) Lookup(ctx context.Context, domain string, strategy DomainStrategy) ([]netip.Addr, error) {
	return nil, os.ErrInvalid
}

func readMessage(reader io.Reader) (*dns.Msg, error) {
	var responseLen uint16
	err := binary.Read(reader, binary.BigEndian, &responseLen)
	if err != nil {
		return nil, err
	}
	if responseLen < 10 {
		return nil, dns.ErrShortRead
	}
	buffer := buf.NewSize(int(responseLen))
	defer buffer.Release()
	_, err = buffer.ReadFullFrom(reader, int(responseLen))
	if err != nil {
		return nil, err
	}
	var message dns.Msg
	err = message.Unpack(buffer.Bytes())
	return &message, err
}

func writeMessage(writer io.Writer, messageId uint16, message *dns.Msg) error {
	requestLen := message.Len()
	buffer := buf.NewSize(3 + requestLen)
	defer buffer.Release()
	common.Must(binary.Write(buffer, binary.BigEndian, uint16(requestLen)))
	exMessage := *message
	exMessage.Id = messageId
	exMessage.Compress = true
	rawMessage, err := exMessage.PackBuffer(buffer.FreeBytes())
	if err != nil {
		return err
	}
	buffer.Truncate(2 + len(rawMessage))
	return common.Error(writer.Write(buffer.Bytes()))
}
