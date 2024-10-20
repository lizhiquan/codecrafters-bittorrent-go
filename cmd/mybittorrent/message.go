package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

type HandshakeMessage struct {
	Protocol string
	Reserved [8]byte
	InfoHash []byte
	PeerID   []byte
}

func marshalHandshakeMessage(w io.Writer, m *HandshakeMessage) error {
	if _, err := w.Write([]byte{byte(len(m.Protocol))}); err != nil {
		return fmt.Errorf("write protocol length: %w", err)
	}

	if _, err := w.Write([]byte(m.Protocol)); err != nil {
		return fmt.Errorf("write protocol: %w", err)
	}

	if _, err := w.Write(m.Reserved[:]); err != nil {
		return fmt.Errorf("write reserved: %w", err)
	}

	if _, err := w.Write(m.InfoHash); err != nil {
		return fmt.Errorf("write info hash: %w", err)
	}

	if _, err := w.Write(m.PeerID); err != nil {
		return fmt.Errorf("write peer id: %w", err)
	}

	return nil
}

func unmarshalHandshakeMessage(r io.Reader, m *HandshakeMessage) error {
	reader := bufio.NewReader(r)

	protocolLength, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("read protocol length: %w", err)
	}

	protocol := make([]byte, protocolLength)
	if _, err := io.ReadFull(reader, protocol); err != nil {
		return fmt.Errorf("read protocol: %w", err)
	}
	m.Protocol = string(protocol)

	if _, err := io.ReadFull(reader, m.Reserved[:]); err != nil {
		return fmt.Errorf("read reserved: %w", err)
	}

	m.InfoHash = make([]byte, 20)
	if _, err := io.ReadFull(reader, m.InfoHash); err != nil {
		return fmt.Errorf("read info hash: %w", err)
	}

	m.PeerID = make([]byte, 20)
	if _, err := io.ReadFull(reader, m.PeerID); err != nil {
		return fmt.Errorf("read peer id: %w", err)
	}

	return nil
}

type PeerMessage struct {
	ID      byte
	Payload []byte
}

const (
	IDChoke byte = iota
	IDUnchoke
	IDInterested
	IDNotInterested
	IDHave
	IDBitfield
	IDRequest
	IDPiece
	IDCancel
	IDKeepAlive
)

func unmarshalPeerMessage(r io.Reader, m *PeerMessage) error {
	reader := bufio.NewReader(r)

	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, lengthBytes); err != nil {
		return fmt.Errorf("read length: %w", err)
	}
	length := binary.BigEndian.Uint32(lengthBytes)

	if length == 0 {
		m.ID = IDKeepAlive
		return nil
	}

	id, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("read id: %w", err)
	}
	m.ID = id

	m.Payload = make([]byte, length-1)
	if _, err := io.ReadFull(reader, m.Payload); err != nil {
		return fmt.Errorf("read payload: %w", err)
	}

	return nil
}

func marshalPeerMessage(w io.Writer, m *PeerMessage) error {
	lengthBytes := make([]byte, 4)
	length := 0
	if m.ID != IDKeepAlive {
		length = 1 + len(m.Payload)
	}
	binary.BigEndian.PutUint32(lengthBytes, uint32(length))
	if _, err := w.Write(lengthBytes); err != nil {
		return fmt.Errorf("write length: %w", err)
	}

	if m.ID == IDKeepAlive {
		return nil
	}

	if _, err := w.Write([]byte{m.ID}); err != nil {
		return fmt.Errorf("write id: %w", err)
	}

	if _, err := w.Write(m.Payload); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	return nil
}

type RequestPayload struct {
	Index  uint32
	Begin  uint32
	Length uint32
}

func (p *RequestPayload) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, p.Index); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, p.Begin); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, p.Length); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type PiecePayload struct {
	Index uint32
	Begin uint32
	Block []byte
}

func (p *PiecePayload) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)
	if err := binary.Read(buf, binary.BigEndian, &p.Index); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &p.Begin); err != nil {
		return err
	}
	p.Block = buf.Bytes()
	return nil
}
