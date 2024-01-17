package tunnel

import (
	"net"

	"github.com/yaling888/clash/common/pool"
)

type packet struct {
	pc      net.PacketConn
	rAddr   net.Addr
	payload *[]byte
}

func (c *packet) Data() *[]byte {
	return c.payload
}

// WriteBack write UDP packet with source(ip, port) = `addr`
func (c *packet) WriteBack(b []byte, _ net.Addr) (n int, err error) {
	return c.pc.WriteTo(b, c.rAddr)
}

// LocalAddr returns the source IP/Port of UDP Packet
func (c *packet) LocalAddr() net.Addr {
	return c.rAddr
}

func (c *packet) Drop() {
	pool.PutNetBuf(c.payload)
	c.payload = nil
}
