package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
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
const TCP_ACK Command = 0x0006
const TCP_RESET_UNIT Command = 0x000A
const TCP_NAK Command = 0x0015
const TCP_DISCONNECT Command = 0x0017
const TCP_GET_MADI_CHANNEL_NAMES Command = 0x1000
const TCP_SET_MADI_CHANNEL_NAMES Command = 0x1001

type ChannelNames struct {
	ChannelName [NumInputChannels][ChannelNameNumLines][ChannelNameTotalNumBytesPerLine]uint8
}

func (c *ChannelNames) GetChannelName(channel int) (string, string) {
	len1 := bytes.IndexByte(c.ChannelName[channel][0][:], 0)
	line1 := string(c.ChannelName[channel][0][:len1])

	len2 := bytes.IndexByte(c.ChannelName[channel][1][:], 0)
	line2 := string(c.ChannelName[channel][1][:len2])

	return line1, line2
}

func TrimString(str string) [ChannelNameTotalNumBytesPerLine]uint8 {
	out := [ChannelNameTotalNumBytesPerLine]uint8{0, 0, 0, 0, 0, 0, 0, 0, 0}

	copy(out[:], []uint8(str[0:uint8(math.Min(ChannelNameLength, float64(len(str))))]))

	return out
}

func (c *ChannelNames) SetChannelName(channel int, line1 string, line2 string) {
	line1Trimmed := TrimString(line1)
	copy(c.ChannelName[channel][0][:], line1Trimmed[:])

	line2Trimmed := TrimString(line2)
	copy(c.ChannelName[channel][1][:], line2Trimmed[:])
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
	return c.SendMessage(TCP_RESET_UNIT, nil)
	// return c.SendCommand(TCP_RESET_UNIT, nil)
}

func (c *Conn) SetMadiChannelNames(channelNames ChannelNames) error {
	err := c.SendCommand(TCP_SET_MADI_CHANNEL_NAMES, channelNames)
	if err != nil {
		return err
	}

	return c.SendCommand(TCP_DISCONNECT, nil)
}

func (c *Conn) GetMadiChannelNames() (ChannelNames, error) {
	channelNames := ChannelNames{}

	err := c.SendMessage(TCP_GET_MADI_CHANNEL_NAMES, nil)
	if err != nil {
		return ChannelNames{}, err
	}

	err = c.conn.SetReadDeadline(time.Now().Add(time.Second * 3))
	if err != nil {
		return ChannelNames{}, err
	}

	initialCommand := [1]byte{}
	_, err = io.ReadFull(c.conn, initialCommand[:])
	if err != nil {
		return ChannelNames{}, err
	}

	if initialCommand[0] != 0 {
		return ChannelNames{}, fmt.Errorf("unknown response %#X", initialCommand[0])
	}

	header := [8]byte{}
	header[0] = initialCommand[0]

	_, err = io.ReadFull(c.conn, header[1:])
	if err != nil {
		return ChannelNames{}, err
	}

	command := binary.LittleEndian.Uint16(header[0:])
	numBytes := binary.LittleEndian.Uint16(header[2:])
	checksum := binary.LittleEndian.Uint32(header[4:])

	switch command {
	case uint16(TCP_GET_MADI_CHANNEL_NAMES):
		dataSize := binary.Size(channelNames)

		if int(numBytes) != dataSize {
			return ChannelNames{}, fmt.Errorf("expected body length of %d, but got %d", dataSize, numBytes)
		}

		body := make([]byte, numBytes)
		_, err = io.ReadFull(c.conn, body)
		if err != nil {
			return ChannelNames{}, err
		}

		runningChecksum := uint32(0)
		for _, v := range header[0:4] {
			runningChecksum += uint32(v)
		}

		for _, v := range body {
			runningChecksum += uint32(v)
		}

		if runningChecksum+checksum != 0 {
			return ChannelNames{}, fmt.Errorf("invalid checksum")
		}

		bodyReader := bytes.NewReader(body)
		err = binary.Read(bodyReader, binary.LittleEndian, &channelNames)
		if err != nil {
			return ChannelNames{}, err
		}

		err = c.SendCommand(TCP_DISCONNECT, nil)
		if err != nil {
			return ChannelNames{}, err
		}

		return channelNames, nil

	case uint16(TCP_NAK):
		return ChannelNames{}, fmt.Errorf("received TCP_NAK")

	default:
		return ChannelNames{}, fmt.Errorf("unknown response %#X", command)
	}
}

func (c *Conn) SendCommand(command Command, body any) error {
	err := c.SendMessage(command, body)
	if err != nil {
		return err
	}

	err = c.conn.SetReadDeadline(time.Now().Add(time.Second * 3))
	if err != nil {
		return err
	}

	header := [1]byte{}

	_, err = io.ReadFull(c.conn, header[:])
	if err != nil {
		return err
	}

	switch header[0] {
	case byte(TCP_ACK):
		return nil

	case byte(TCP_NAK):
		return fmt.Errorf("received TCP_NAK")

	default:
		return fmt.Errorf("unknown response %#X", command)
	}
}

func (c *Conn) SendMessage(command Command, body any) error {
	msg, err := ConstructMessage(command, body)
	if err != nil {
		return err
	}

	_, err = c.conn.Write(msg)
	return err
}

func ConstructMessage(command Command, body any) ([]byte, error) {
	bodySize := 0
	if body != nil {
		bodySize = binary.Size(body)
	}

	buf := new(bytes.Buffer)

	err := binary.Write(buf, binary.LittleEndian, command)
	if err != nil {
		return []byte{}, err
	}

	err = binary.Write(buf, binary.LittleEndian, uint16(bodySize))
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

	checksum := uint32(0)
	for _, v := range bytes {
		checksum += uint32(v)
	}

	binary.LittleEndian.PutUint32(bytes[4:], ^checksum+1)

	return bytes, nil
}

func main() {
	conn, err := NewConn("10.51.62.211")
	if err != nil {
		panic(err)
	}

	names, err := conn.GetMadiChannelNames()
	if err != nil {
		panic(err)
	}

	names.SetChannelName(6, "HELLO", "NICK")

	err = conn.SetMadiChannelNames(names)
	if err != nil {
		panic(err)
	}

	line1, line2 := names.GetChannelName(2)

	fmt.Println(line1)
	fmt.Println(line2)

	time.Sleep(time.Second)

	err = conn.Reset()
	if err != nil {
		fmt.Println(err)
	}
}
