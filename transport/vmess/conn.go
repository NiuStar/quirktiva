package vmess

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/fnv"
	"io"
	R "math/rand/v2"
	"net"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/yaling888/quirktiva/common/pool"
)

// Conn wrapper a net.Conn with vmess protocol
type Conn struct {
	net.Conn
	reader      io.Reader
	writer      io.Writer
	dst         *DstAddr
	id          *ID
	reqBodyIV   []byte
	reqBodyKey  []byte
	respBodyIV  []byte
	respBodyKey []byte
	respV       byte
	security    byte
	option      byte
	isAead      bool

	received bool
}

func (vc *Conn) Write(b []byte) (int, error) {
	return vc.writer.Write(b)
}

func (vc *Conn) Read(b []byte) (int, error) {
	if vc.received {
		return vc.reader.Read(b)
	}

	if err := vc.recvResponse(); err != nil {
		return 0, err
	}
	vc.received = true
	return vc.reader.Read(b)
}

func (vc *Conn) sendRequest() error {
	timestamp := time.Now()

	mBuf := pool.BufferWriter{}

	if !vc.isAead {
		h := hmac.New(md5.New, vc.id.UUID.Bytes())
		_ = binary.Write(h, binary.BigEndian, uint64(timestamp.Unix()))
		mBuf.PutSlice(h.Sum(nil))
	}

	buf := pool.BufferWriter{}

	// Ver IV Key V Opt
	buf.PutUint8(Version)
	buf.PutSlice(vc.reqBodyIV[:])
	buf.PutSlice(vc.reqBodyKey[:])
	buf.PutUint8(vc.respV)
	buf.PutUint8(vc.option)

	p := R.IntN(16)
	// P Sec Reserve Cmd
	buf.PutUint8(byte(p<<4) | vc.security)
	buf.PutUint8(0)
	if vc.dst.UDP {
		buf.PutUint8(CommandUDP)
	} else {
		buf.PutUint8(CommandTCP)
	}

	// Port AddrType Addr
	buf.PutUint16be(uint16(vc.dst.Port))
	buf.PutUint8(vc.dst.AddrType)
	buf.PutSlice(vc.dst.Addr)

	// padding
	if p > 0 {
		_ = buf.ReadFull(rand.Reader, p)
	}

	fnv1a := fnv.New32a()
	_, _ = fnv1a.Write(buf.Bytes())
	buf.PutSlice(fnv1a.Sum(nil))

	if !vc.isAead {
		block, err := aes.NewCipher(vc.id.CmdKey)
		if err != nil {
			return err
		}

		stream := cipher.NewCFBEncrypter(block, hashTimestamp(timestamp))
		stream.XORKeyStream(buf.Bytes(), buf.Bytes())
		mBuf.PutSlice(buf.Bytes())
		_, err = vc.Conn.Write(mBuf.Bytes())
		return err
	}

	var fixedLengthCmdKey [16]byte
	copy(fixedLengthCmdKey[:], vc.id.CmdKey)
	vmessout := sealVMessAEADHeader(fixedLengthCmdKey, buf.Bytes(), timestamp)
	_, err := vc.Conn.Write(vmessout)
	return err
}

func (vc *Conn) recvResponse() error {
	var buf []byte
	if !vc.isAead {
		block, err := aes.NewCipher(vc.respBodyKey[:])
		if err != nil {
			return err
		}

		stream := cipher.NewCFBDecrypter(block, vc.respBodyIV[:])
		buf = make([]byte, 4)
		_, err = io.ReadFull(vc.Conn, buf)
		if err != nil {
			return err
		}
		stream.XORKeyStream(buf, buf)
	} else {
		aeadResponseHeaderLengthEncryptionKey := kdf(vc.respBodyKey[:], kdfSaltConstAEADRespHeaderLenKey)[:16]
		aeadResponseHeaderLengthEncryptionIV := kdf(vc.respBodyIV[:], kdfSaltConstAEADRespHeaderLenIV)[:12]

		aeadResponseHeaderLengthEncryptionKeyAESBlock, _ := aes.NewCipher(aeadResponseHeaderLengthEncryptionKey)
		aeadResponseHeaderLengthEncryptionAEAD, _ := cipher.NewGCM(aeadResponseHeaderLengthEncryptionKeyAESBlock)

		aeadEncryptedResponseHeaderLength := make([]byte, 18)
		if _, err := io.ReadFull(vc.Conn, aeadEncryptedResponseHeaderLength); err != nil {
			return err
		}

		decryptedResponseHeaderLengthBinaryBuffer, err := aeadResponseHeaderLengthEncryptionAEAD.Open(nil, aeadResponseHeaderLengthEncryptionIV, aeadEncryptedResponseHeaderLength[:], nil)
		if err != nil {
			return err
		}

		decryptedResponseHeaderLength := binary.BigEndian.Uint16(decryptedResponseHeaderLengthBinaryBuffer)
		aeadResponseHeaderPayloadEncryptionKey := kdf(vc.respBodyKey[:], kdfSaltConstAEADRespHeaderPayloadKey)[:16]
		aeadResponseHeaderPayloadEncryptionIV := kdf(vc.respBodyIV[:], kdfSaltConstAEADRespHeaderPayloadIV)[:12]
		aeadResponseHeaderPayloadEncryptionKeyAESBlock, _ := aes.NewCipher(aeadResponseHeaderPayloadEncryptionKey)
		aeadResponseHeaderPayloadEncryptionAEAD, _ := cipher.NewGCM(aeadResponseHeaderPayloadEncryptionKeyAESBlock)

		encryptedResponseHeaderBuffer := make([]byte, decryptedResponseHeaderLength+16)
		if _, err := io.ReadFull(vc.Conn, encryptedResponseHeaderBuffer); err != nil {
			return err
		}

		buf, err = aeadResponseHeaderPayloadEncryptionAEAD.Open(nil, aeadResponseHeaderPayloadEncryptionIV, encryptedResponseHeaderBuffer, nil)
		if err != nil {
			return err
		}

		if len(buf) < 4 {
			return errors.New("unexpected buffer length")
		}
	}

	if buf[0] != vc.respV {
		return errors.New("unexpected response header")
	}

	if buf[2] != 0 {
		return errors.New("dynamic port is not supported now")
	}

	return nil
}

func hashTimestamp(t time.Time) []byte {
	md5hash := md5.New()
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(t.Unix()))
	md5hash.Write(ts)
	md5hash.Write(ts)
	md5hash.Write(ts)
	md5hash.Write(ts)
	return md5hash.Sum(nil)
}

// newConn return a Conn instance
func newConn(conn net.Conn, id *ID, dst *DstAddr, security Security, isAead bool) (*Conn, error) {
	randBytes := make([]byte, 33)
	_, _ = rand.Read(randBytes)
	reqBodyIV := make([]byte, 16)
	reqBodyKey := make([]byte, 16)
	copy(reqBodyIV[:], randBytes[:16])
	copy(reqBodyKey[:], randBytes[16:32])
	respV := randBytes[32]
	option := OptionChunkStream

	var (
		respBodyKey []byte
		respBodyIV  []byte
	)

	if isAead {
		bodyKey := sha256.Sum256(reqBodyKey)
		bodyIV := sha256.Sum256(reqBodyIV)
		respBodyKey = bodyKey[:16]
		respBodyIV = bodyIV[:16]
	} else {
		bodyKey := md5.Sum(reqBodyKey)
		bodyIV := md5.Sum(reqBodyIV)
		respBodyKey = bodyKey[:]
		respBodyIV = bodyIV[:]
	}

	var writer io.Writer
	var reader io.Reader
	switch security {
	case SecurityZero:
		security = SecurityNone
		if !dst.UDP {
			reader = conn
			writer = conn
			option = 0
		} else {
			reader = newChunkReader(conn)
			writer = newChunkWriter(conn)
		}
	case SecurityNone:
		reader = newChunkReader(conn)
		writer = newChunkWriter(conn)
	case SecurityAES128GCM:
		block, _ := aes.NewCipher(reqBodyKey[:])
		aead, _ := cipher.NewGCM(block)
		writer = newAEADWriter(conn, aead, reqBodyIV[:])

		block, _ = aes.NewCipher(respBodyKey[:])
		aead, _ = cipher.NewGCM(block)
		reader = newAEADReader(conn, aead, respBodyIV[:])
	case SecurityCHACHA20POLY1305:
		key := make([]byte, 32)
		t := md5.Sum(reqBodyKey[:])
		copy(key, t[:])
		t = md5.Sum(key[:16])
		copy(key[16:], t[:])
		aead, _ := chacha20poly1305.New(key)
		writer = newAEADWriter(conn, aead, reqBodyIV[:])

		t = md5.Sum(respBodyKey[:])
		copy(key, t[:])
		t = md5.Sum(key[:16])
		copy(key[16:], t[:])
		aead, _ = chacha20poly1305.New(key)
		reader = newAEADReader(conn, aead, respBodyIV[:])
	}

	c := &Conn{
		Conn:        conn,
		id:          id,
		dst:         dst,
		reqBodyIV:   reqBodyIV,
		reqBodyKey:  reqBodyKey,
		respV:       respV,
		respBodyIV:  respBodyIV[:],
		respBodyKey: respBodyKey[:],
		reader:      reader,
		writer:      writer,
		security:    security,
		option:      option,
		isAead:      isAead,
	}
	if err := c.sendRequest(); err != nil {
		return nil, err
	}
	return c, nil
}
