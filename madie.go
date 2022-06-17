package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const MadieTcpPort = "9760"
const NumInputChannels = 64
const ChannelNameNumLines = 2

const ChannelNameLength = 8
const ChannelNameNumBytesPerLine = ChannelNameLength + 1
const ChannelNameNumReservedBytesPerLine = 3
const ChannelNameTotalNumBytesPerLine = ChannelNameNumBytesPerLine + ChannelNameNumReservedBytesPerLine

type Command uint16

const TCP_NOP Command = 0x0000
// The AMP1-16-M uses 0x00000017 and shares a similar looking protocol
const TCP_DISCONNECT Command = 0x0004
const TCP_ACK Command = 0x0006
const TCP_RESET_UNIT Command = 0x000A
const TCP_NAK Command = 0x0015
const TCP_GET_MADI_CHANNEL_NAMES Command = 0x1000
const TCP_SET_MADI_CHANNEL_NAMES Command = 0x1001

type ChannelNames struct {
	channelName [NumInputChannels][ChannelNameNumLines]string
}

func (c *ChannelNames) IntoRaw() RawChannelNames {
	raw := RawChannelNames{}
	for i, lines := range c.channelName {
		for j, name := range lines {
			copy(raw.channelName[i][j][:], []uint8(name[0:ChannelNameLength]))
		}
	}

	return raw
}

type RawChannelNames struct {
	channelName [NumInputChannels][ChannelNameNumLines][ChannelNameTotalNumBytesPerLine]uint8
}

func (c *RawChannelNames) IntoChannelNames() ChannelNames {
	out := ChannelNames{}
	for i, lines := range c.channelName {
		for j, name := range lines {
			len := bytes.IndexByte(name[:], 0)
			out.channelName[i][j] = string(name[:len])
		}
	}

	return out
}

type Conn struct {
	conn net.Conn
}

func NewConn(host string) (Conn, error) {
	address := net.JoinHostPort(host, MadieTcpPort)

	tcpConn, err := net.Dial("tcp", address)
	if err != nil {
		return Conn{}, err
	}

	return Conn{conn: tcpConn}, nil
}

func (c *Conn) Reset() error {
	err := c.SendAndReceive(TCP_RESET_UNIT, nil, nil)
	if err != nil {
		return err
	}

	return c.SendAndReceive(TCP_DISCONNECT, nil, nil)
}

func (c *Conn) SetMadiChannelNames(channelNames ChannelNames) error {
	err := c.SendAndReceive(TCP_SET_MADI_CHANNEL_NAMES, channelNames.IntoRaw(), nil)
	if err != nil {
		return err
	}

	return c.SendAndReceive(TCP_DISCONNECT, nil, nil)
}

func (c *Conn) GetMadiChannelNames() (ChannelNames, error) {
	rawChannelNames := RawChannelNames{}

	err := c.SendAndReceive(TCP_GET_MADI_CHANNEL_NAMES, nil, rawChannelNames)
	if err != nil {
		return ChannelNames{}, err
	}

	err = c.SendAndReceive(TCP_DISCONNECT, nil, nil)
	if err != nil {
		return ChannelNames{}, err
	}

	return rawChannelNames.IntoChannelNames(), nil
}

func (c *Conn) SendAndReceive(command Command, requestBody any, responseBody any) error {
	err := c.SendMessage(command, requestBody)
	if err != nil {
		return err
	}

	return c.ReceiveMessage(responseBody)
}

func (c *Conn) SendMessage(command Command, body any) error {
	msg, err := ConstructMessage(command, body)
	if err != nil {
		return err
	}

	_, err = c.conn.Write(msg)
	if err != nil {
		return err
	}

	return nil
}

func (c *Conn) ReceiveMessage(data any) error {
	err := c.conn.SetReadDeadline(time.Now().Add(time.Second * 3))
	if err != nil {
		return err
	}

	header := [8]byte{}

	_, err = io.ReadFull(c.conn, header[:])
	if err != nil {
		return err
	}

	command := binary.LittleEndian.Uint16(header[0:])
	numBytes := binary.LittleEndian.Uint16(header[2:])
	checksum := binary.LittleEndian.Uint32(header[4:])

	switch command {
	case uint16(TCP_ACK):
		dataSize := 0
		if data != nil {
			dataSize = binary.Size(data)
		}

		if int(numBytes) != dataSize {
			return fmt.Errorf("expected body length of %d, but got %d", dataSize, numBytes)
		}

		body := make([]byte, numBytes)
		_, err = io.ReadFull(c.conn, body)
		if err != nil {
			return err
		}

		runningChecksum := uint32(0)
		for _, v := range header[0:4] {
			runningChecksum += uint32(v)
		}

		for _, v := range body {
			runningChecksum += uint32(v)
		}

		if runningChecksum+checksum != 0 {
			return fmt.Errorf("invalid checksum")
		}

		if dataSize != 0 {
			bodyReader := bytes.NewReader(body)
			return binary.Read(bodyReader, binary.LittleEndian, data)
		}

		return nil

	case uint16(TCP_NAK):
		return fmt.Errorf("received TCP_NAK")

	default:
		return fmt.Errorf("unknown response %#X", command)
	}
}

func ConstructMessage(command Command, body any) ([]byte, error) {
	buf := new(bytes.Buffer)

	err := binary.Write(buf, binary.LittleEndian, command)
	if err != nil {
		return []byte{}, err
	}

	err = binary.Write(buf, binary.LittleEndian, uint16(0))
	if err != nil {
		return []byte{}, err
	}

	err = binary.Write(buf, binary.LittleEndian, int32(0))
	if err != nil {
		return []byte{}, err
	}

	if body != nil {
		err = binary.Write(buf, binary.LittleEndian, body)
		if err != nil {
			return []byte{}, err
		}
	}

	bytes := buf.Bytes()

	binary.LittleEndian.PutUint16(bytes[2:], uint16(buf.Len()-8))

	checksum := uint32(0)
	for _, v := range bytes {
		checksum += uint32(v)
	}

	binary.LittleEndian.PutUint32(bytes[4:], ^checksum+1)

	return bytes, nil
}

func main() {
	conn, err := NewConn("127.0.0.1")
	if err != nil {
		fmt.Println(err)
	}

	err = conn.Reset()
	if err != nil {
		fmt.Println(err)
	}
}
