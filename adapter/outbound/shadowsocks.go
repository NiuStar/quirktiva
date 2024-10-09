package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/yaling888/quirktiva/common/structure"
	"github.com/yaling888/quirktiva/component/dialer"
	C "github.com/yaling888/quirktiva/constant"
	"github.com/yaling888/quirktiva/transport/shadowsocks/core"
	obfs "github.com/yaling888/quirktiva/transport/simple-obfs"
	"github.com/yaling888/quirktiva/transport/socks5"
	v2rayObfs "github.com/yaling888/quirktiva/transport/v2ray-plugin"
)

var _ C.ProxyAdapter = (*ShadowSocks)(nil)

type ShadowSocks struct {
	*Base
	cipher core.Cipher

	// obfs
	obfsMode    string
	obfsOption  *simpleObfsOption
	v2rayOption *v2rayObfs.Option
}

type ShadowSocksOption struct {
	BasicOption
	Name             string         `proxy:"name"`
	Server           string         `proxy:"server"`
	Port             int            `proxy:"port"`
	Password         string         `proxy:"password"`
	Cipher           string         `proxy:"cipher"`
	UDP              bool           `proxy:"udp,omitempty"`
	Plugin           string         `proxy:"plugin,omitempty"`
	PluginOpts       map[string]any `proxy:"plugin-opts,omitempty"`
	RandomHost       bool           `proxy:"rand-host,omitempty"`
	RemoteDnsResolve bool           `proxy:"remote-dns-resolve,omitempty"`
}

type simpleObfsOption struct {
	Mode       string `obfs:"mode,omitempty"`
	Host       string `obfs:"host,omitempty"`
	RandomHost bool   `obfs:"rand-host,omitempty"`
}

type v2rayObfsOption struct {
	Mode           string            `obfs:"mode"`
	Host           string            `obfs:"host,omitempty"`
	Path           string            `obfs:"path,omitempty"`
	TLS            bool              `obfs:"tls,omitempty"`
	Headers        map[string]string `obfs:"headers,omitempty"`
	SkipCertVerify bool              `obfs:"skip-cert-verify,omitempty"`
	Mux            bool              `obfs:"mux,omitempty"`
}

// StreamConn implements C.ProxyAdapter
func (ss *ShadowSocks) StreamConn(c net.Conn, metadata *C.Metadata) (net.Conn, error) {
	switch ss.obfsMode {
	case "tls":
		c = obfs.NewTLSObfs(c, ss.obfsOption.Host)
	case "http":
		_, port, _ := net.SplitHostPort(ss.addr)
		c = obfs.NewHTTPObfs(c, ss.obfsOption.Host, port, ss.obfsOption.RandomHost)
	case "websocket":
		var err error
		c, err = v2rayObfs.NewV2rayObfs(c, ss.v2rayOption)
		if err != nil {
			return nil, fmt.Errorf("%s connect error: %w", ss.addr, err)
		}
	}
	c = ss.cipher.StreamConn(c)
	_, err := c.Write(serializesSocksAddr(metadata))
	return c, err
}

// StreamPacketConn implements C.ProxyAdapter
func (ss *ShadowSocks) StreamPacketConn(c net.Conn, _ *C.Metadata) (net.Conn, error) {
	if !IsPacketConn(c) {
		return c, fmt.Errorf("%s connect error: can not convert net.Conn to net.PacketConn", ss.addr)
	}

	addr, err := resolveUDPAddr("udp", ss.addr)
	if err != nil {
		return c, err
	}

	pc := ss.cipher.PacketConn(c.(net.PacketConn))
	return WrapConn(&ssPacketConn{PacketConn: pc, rAddr: addr}), nil
}

// DialContext implements C.ProxyAdapter
func (ss *ShadowSocks) DialContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (_ C.Conn, err error) {
	c, err := dialer.DialContext(ctx, "tcp", ss.addr, ss.Base.DialOptions(opts...)...)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", ss.addr, err)
	}
	tcpKeepAlive(c)

	defer func(cc net.Conn, e error) {
		safeConnClose(cc, e)
	}(c, err)

	c, err = ss.StreamConn(c, metadata)
	return NewConn(c, ss), err
}

// ListenPacketContext implements C.ProxyAdapter
func (ss *ShadowSocks) ListenPacketContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.PacketConn, error) {
	pc, err := dialer.ListenPacket(ctx, "udp", "", ss.Base.DialOptions(opts...)...)
	if err != nil {
		return nil, err
	}

	c, err := ss.StreamPacketConn(WrapConn(pc), metadata)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}

	return NewPacketConn(c.(net.PacketConn), ss), nil
}

func NewShadowSocks(option ShadowSocksOption) (*ShadowSocks, error) {
	addr := net.JoinHostPort(option.Server, strconv.Itoa(option.Port))
	cipher := option.Cipher
	password := option.Password
	ciph, err := core.PickCipher(cipher, nil, password)
	if err != nil {
		return nil, fmt.Errorf("ss %s initialize error: %w", addr, err)
	}

	var v2rayOption *v2rayObfs.Option
	var obfsOption *simpleObfsOption
	obfsMode := ""

	decoder := structure.NewDecoder(structure.Option{TagName: "obfs", WeaklyTypedInput: true})
	if option.Plugin == "obfs" {
		opts := simpleObfsOption{Host: "bing.com", RandomHost: option.RandomHost}
		if err := decoder.Decode(option.PluginOpts, &opts); err != nil {
			return nil, fmt.Errorf("ss %s initialize obfs error: %w", addr, err)
		}

		if opts.Mode != "tls" && opts.Mode != "http" {
			return nil, fmt.Errorf("ss %s obfs mode error: %s", addr, opts.Mode)
		}
		obfsMode = opts.Mode
		obfsOption = &opts
	} else if option.Plugin == "v2ray-plugin" {
		opts := v2rayObfsOption{Host: "bing.com", Mux: true}
		if err := decoder.Decode(option.PluginOpts, &opts); err != nil {
			return nil, fmt.Errorf("ss %s initialize v2ray-plugin error: %w", addr, err)
		}

		if opts.Mode != "websocket" {
			return nil, fmt.Errorf("ss %s obfs mode error: %s", addr, opts.Mode)
		}
		obfsMode = opts.Mode
		v2rayOption = &v2rayObfs.Option{
			Host:       opts.Host,
			Path:       opts.Path,
			Headers:    opts.Headers,
			Mux:        opts.Mux,
			RandomHost: option.RandomHost,
		}

		if opts.TLS {
			v2rayOption.TLS = true
			v2rayOption.SkipCertVerify = opts.SkipCertVerify
		}
	}

	return &ShadowSocks{
		Base: &Base{
			name:  option.Name,
			addr:  addr,
			tp:    C.Shadowsocks,
			udp:   option.UDP,
			iface: option.Interface,
			rmark: option.RoutingMark,
			dns:   option.RemoteDnsResolve,
		},
		cipher: ciph,

		obfsMode:    obfsMode,
		v2rayOption: v2rayOption,
		obfsOption:  obfsOption,
	}, nil
}

type ssPacketConn struct {
	net.PacketConn
	rAddr net.Addr
}

func (spc *ssPacketConn) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	packet, err := socks5.EncodeUDPPacket(socks5.ParseAddrToSocksAddr(addr), b)
	if err != nil {
		return
	}
	return spc.PacketConn.WriteTo(packet[3:], spc.rAddr)
}

func (spc *ssPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, _, e := spc.PacketConn.ReadFrom(b)
	if e != nil {
		return 0, nil, e
	}

	addr := socks5.SplitAddr(b[:n])
	if addr == nil {
		return 0, nil, errors.New("parse addr error")
	}

	udpAddr := addr.UDPAddr()
	if udpAddr == nil {
		return 0, nil, errors.New("parse addr error")
	}

	copy(b, b[len(addr):])
	return n - len(addr), udpAddr, e
}
