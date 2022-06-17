package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
)

const NumInputChnnels = 64;
const ChannelNameNumLines = 2;

const ChannelNameNumBytesPerLine = 9;
const ChannelNameNumReservedBytesPerLine = 3;
const ChannelNameTotalNumBytesPerLine = ChannelNameNumBytesPerLine + ChannelNameNumReservedBytesPerLine;

const TCP_NOK = 0x0000;
const TCP_ACK = 0x0006;
const TCP_RESET_UNIT = 0x000A;
const TCP_NAK = 0x0015;
const TCP_GET_MADI_CHANNEL_NAMES = 0x1000;
const TCP_SET_MADI_CHANNEL_NAMES = 0x1001;

type ChannelNames struct {
	channelName [ChannelNameTotalNumBytesPerLine][ChannelNameNumLines][NumInputChnnels]uint8
}

type Conn struct {
	conn net.Conn
}

func NewConn(host string) (Conn, error) {
	address := net.JoinHostPort(host, "9760")

	tcpConn, err := net.Dial("tcp", address)
	if err != nil {
		return Conn{}, err
	}

	return Conn { conn: tcpConn }, nil
}

func (c *Conn) SendMessage(command uint16, body any) error {
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

func ConstructMessage(command uint16, body any) ([]byte, error) {
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

	binary.LittleEndian.PutUint16(bytes[2:], uint16(buf.Len() - 8))

	checksum := uint32(0)
	for _, v := range bytes {
		checksum += uint32(v)
	}

	binary.LittleEndian.PutUint32(bytes[4:], ^checksum + 1)

	return bytes, nil
}

func main() {
	m, err := ConstructMessage(TCP_ACK, ChannelNames {})

	checksum := uint32(0)
	for _, v := range m[0:4] {
		checksum += uint32(v)
	}

	for _, v := range m[8:] {
		checksum += uint32(v)
	}

	checksum += binary.LittleEndian.Uint32(m[4:])

  fmt.Println(m)
	fmt.Println(err)
	fmt.Println(checksum)
	fmt.Println(binary.LittleEndian.Uint16(m[2:]))
}