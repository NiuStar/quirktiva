package system

import (
	"net"
	"net/netip"

	"github.com/yaling888/clash/common/pool"
	"github.com/yaling888/clash/listener/tun/ipstack/system/mars/nat"
)

type packet struct {
	sender *nat.UDP
	lAddr  netip.AddrPort
	data   *pool.Buffer
}

func (pkt *packet) Data() []byte {
	if pkt.data == nil {
		return nil
	}
	return pkt.data.Bytes()
}

func (pkt *packet) WriteBack(b []byte, addr net.Addr) (n int, err error) {
	a := addr.(*net.UDPAddr)
	na, _ := netip.AddrFromSlice(a.IP)
	na = na.WithZone(a.Zone)
	if pkt.lAddr.Addr().Is4() {
		na = na.Unmap()
	}
	return pkt.sender.WriteTo(b, netip.AddrPortFrom(na, uint16(a.Port)), pkt.lAddr)
}

func (pkt *packet) LocalAddr() net.Addr {
	return net.UDPAddrFromAddrPort(pkt.lAddr)
}

func (pkt *packet) Drop() {
	pkt.data.Release()
	pkt.data = nil
}
