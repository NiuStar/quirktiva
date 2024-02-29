//go:build !nogvisor

package gvisor

import (
	"net"
	"net/netip"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"

	"github.com/yaling888/clash/listener/tun/ipstack/gvisor/adapter"
	"github.com/yaling888/clash/listener/tun/ipstack/gvisor/option"
)

type packet struct {
	stack *stack.Stack
	data  *buffer.View
	nicID tcpip.NICID
	lAddr netip.AddrPort
}

func (pkt *packet) Data() *[]byte {
	if pkt.data != nil {
		b := pkt.data.AsSlice()
		return &b
	}
	return nil
}

func (pkt *packet) WriteBack(b []byte, addr net.Addr) (n int, err error) {
	a := addr.(*net.UDPAddr)
	na, _ := netip.AddrFromSlice(a.IP)
	na = na.WithZone(a.Zone)
	if pkt.lAddr.Addr().Is4() {
		na = na.Unmap()
	}

	conn, err := dialUDP(pkt.stack, pkt.nicID, netip.AddrPortFrom(na, uint16(a.Port)), pkt.lAddr)
	if err != nil {
		return
	}

	n, err = conn.Write(b)
	_ = conn.Close()
	return
}

func (pkt *packet) LocalAddr() net.Addr {
	return net.UDPAddrFromAddrPort(pkt.lAddr)
}

func (pkt *packet) Drop() {
	pkt.data.Release()
	pkt.data = nil
}

type forwarder struct {
	handler func(*stack.Stack, stack.TransportEndpointID, *stack.PacketBuffer)

	stack *stack.Stack
}

func newForwarder(
	s *stack.Stack,
	handler func(*stack.Stack, stack.TransportEndpointID, *stack.PacketBuffer),
) *forwarder {
	return &forwarder{
		stack:   s,
		handler: handler,
	}
}

func (f *forwarder) HandlePacket(id stack.TransportEndpointID, pkt *stack.PacketBuffer) bool {
	f.handler(f.stack, id, pkt.IncRef())
	return true
}

func withUDPHandler(handle adapter.UDPHandleFunc) option.Option {
	return func(s *stack.Stack) error {
		udpForwarder := newForwarder(
			s,
			func(
				st *stack.Stack, id stack.TransportEndpointID, pkt *stack.PacketBuffer,
			) {
				handle(st, id, pkt)
			})
		s.SetTransportProtocolHandler(udp.ProtocolNumber, udpForwarder.HandlePacket)
		return nil
	}
}

func dialUDP(s *stack.Stack, id tcpip.NICID, lAddr, rAddr netip.AddrPort) (*gonet.UDPConn, error) {
	if !lAddr.IsValid() || !rAddr.IsValid() {
		return nil, net.InvalidAddrError("invalid address")
	}

	src := &tcpip.FullAddress{
		NIC:  id,
		Addr: tcpip.AddrFromSlice(lAddr.Addr().AsSlice()),
		Port: lAddr.Port(),
	}

	dst := &tcpip.FullAddress{
		NIC:  id,
		Addr: tcpip.AddrFromSlice(rAddr.Addr().AsSlice()),
		Port: rAddr.Port(),
	}

	networkProtocolNumber := header.IPv4ProtocolNumber
	if lAddr.Addr().Is6() || rAddr.Addr().Is6() {
		networkProtocolNumber = header.IPv6ProtocolNumber
	}

	return gonet.DialUDP(s, src, dst, networkProtocolNumber)
}
