package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"

	bencode "github.com/jackpal/bencode-go"
)

var peerID func() []byte = sync.OnceValue(func() []byte {
	bytes := make([]byte, 20)
	_, err := rand.Read(bytes)
	if err != nil {
		panic(err)
	}
	return bytes
})

func getPeers(trackerURL string, infoHash []byte, left int) ([]string, error) {
	req, err := http.NewRequest("GET", trackerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	query := req.URL.Query()
	query.Add("info_hash", string(infoHash))
	query.Add("peer_id", hex.EncodeToString(peerID())[:20])
	query.Add("port", "6881")
	query.Add("uploaded", "0")
	query.Add("downloaded", "0")
	query.Add("left", strconv.Itoa(left))
	query.Add("compact", "1")
	req.URL.RawQuery = query.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	defer resp.Body.Close()

	var response TrackerResponse
	err = bencode.Unmarshal(resp.Body, &response)
	if err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return response.PeerList(), nil
}

type task struct {
	piecePath  string
	pieceIndex int
	pieceHash  []byte
}

func dialPeer(peerAddr string, infoHash []byte, isMagnet bool) (net.Conn, *TorrentInfo, error) {
	conn, err := net.Dial("tcp", peerAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("dial: %w", err)
	}

	handshakeMessage := HandshakeMessage{
		Protocol: "BitTorrent protocol",
		InfoHash: infoHash,
		PeerID:   peerID(),
	}
	if isMagnet {
		handshakeMessage.SetExtension()
	}
	if err := marshalHandshakeMessage(conn, &handshakeMessage); err != nil {
		return nil, nil, fmt.Errorf("marshal handshake: %w", err)
	}
	if err := unmarshalHandshakeMessage(conn, &handshakeMessage); err != nil {
		return nil, nil, fmt.Errorf("unmarshal handshake: %w", err)
	}

	if isMagnet && !handshakeMessage.IsExtension() {
		return nil, nil, fmt.Errorf("extension not supported")
	}

	// read bitfield
	var m PeerMessage
	if err := unmarshalPeerMessage(conn, &m); err != nil {
		return nil, nil, fmt.Errorf("unmarshal bitfield: %w", err)
	}
	if m.ID != IDBitfield {
		return nil, nil, fmt.Errorf("expect bitfield")
	}

	if !isMagnet {
		return conn, nil, nil
	}

	// extension handshake
	extensionPayload := ExtensionPayload{
		MessageID: 0,
		Message: map[string]any{
			"m": map[string]any{
				"ut_metadata": 1,
			},
		},
	}
	payload, err := extensionPayload.MarshalBinary()
	if err != nil {
		return nil, nil, fmt.Errorf("marshal extension: %w", err)
	}
	m = PeerMessage{
		ID:      IDExtension,
		Payload: payload,
	}
	if err := marshalPeerMessage(conn, &m); err != nil {
		return nil, nil, fmt.Errorf("marshal extension: %w", err)
	}
	if err := unmarshalPeerMessage(conn, &m); err != nil {
		return nil, nil, fmt.Errorf("unmarshal extension: %w", err)
	}
	if m.ID != IDExtension {
		return nil, nil, fmt.Errorf("expect extension")
	}

	if err := extensionPayload.UnmarshalBinary(m.Payload); err != nil {
		return nil, nil, fmt.Errorf("unmarshal extension: %w", err)
	}

	// request metadata
	peerExtID := extensionPayload.Message.(map[string]any)["m"].(map[string]any)["ut_metadata"].(int64)
	extensionPayload = ExtensionPayload{
		MessageID: byte(peerExtID),
		Message: map[string]any{
			"msg_type": 0,
			"piece":    0,
		},
	}
	payload, err = extensionPayload.MarshalBinary()
	if err != nil {
		return nil, nil, fmt.Errorf("marshal extension: %w", err)
	}
	m = PeerMessage{
		ID:      IDExtension,
		Payload: payload,
	}
	if err := marshalPeerMessage(conn, &m); err != nil {
		return nil, nil, fmt.Errorf("marshal extension: %w", err)
	}
	if err := unmarshalPeerMessage(conn, &m); err != nil {
		return nil, nil, fmt.Errorf("unmarshal extension: %w", err)
	}
	if err := extensionPayload.UnmarshalBinary(m.Payload); err != nil {
		return nil, nil, fmt.Errorf("unmarshal extension: %w", err)
	}

	size := extensionPayload.Message.(map[string]any)["total_size"].(int64)
	metadataBytes := m.Payload[len(m.Payload)-int(size):]
	var torrentInfo TorrentInfo
	if err := bencode.Unmarshal(bytes.NewReader(metadataBytes), &torrentInfo); err != nil {
		return nil, nil, fmt.Errorf("unmarshal metadata: %w", err)
	}

	return conn, &torrentInfo, nil
}

func downloadPiece(conn net.Conn, torrentInfo *TorrentInfo, taskCh chan task, wg *sync.WaitGroup) {
	// send interested
	m := PeerMessage{ID: IDInterested}
	if err := marshalPeerMessage(conn, &m); err != nil {
		panic(err)
	}

	// read unchoke
	if err := unmarshalPeerMessage(conn, &m); err != nil {
		panic(err)
	}
	if m.ID != IDUnchoke {
		panic("expect unchock")
	}

	for task := range taskCh {
	StartDownloadPiece:
		fmt.Printf("downloading piece %d\n", task.pieceIndex)

		// create piece file
		pieceFile, err := os.Create(task.piecePath)
		if err != nil {
			panic(err)
		}

		// download piece
		size := torrentInfo.Length
		pieceSize := torrentInfo.PieceLength
		pieceCount := int(math.Ceil(float64(size) / float64(pieceSize)))
		if task.pieceIndex == pieceCount-1 {
			pieceSize = size % pieceSize
		}
		blockSize := 16 * 1024 // 16KB
		blockCount := int(math.Ceil(float64(pieceSize) / float64(blockSize)))
		hash := sha1.New()
		for i := 0; i < blockCount; i++ {
			length := blockSize
			if i == blockCount-1 {
				length = pieceSize - (blockCount-1)*blockSize
			}

			// send request
			payload, err := (&RequestPayload{
				Index:  uint32(task.pieceIndex),
				Begin:  uint32(i * blockSize),
				Length: uint32(length),
			}).MarshalBinary()
			if err != nil {
				panic(err)
			}
			m = PeerMessage{ID: IDRequest, Payload: payload}
			if err := marshalPeerMessage(conn, &m); err != nil {
				panic(err)
			}

			// read piece
			if err := unmarshalPeerMessage(conn, &m); err != nil {
				panic(err)
			}
			if m.ID != IDPiece {
				panic("expect piece")
			}

			var piecePayload PiecePayload
			if err := piecePayload.UnmarshalBinary(m.Payload); err != nil {
				panic(err)
			}

			if _, err := pieceFile.Write(piecePayload.Block); err != nil {
				panic(err)
			}

			if _, err := hash.Write(piecePayload.Block); err != nil {
				panic(err)
			}
		}

		pieceFile.Close()

		// verify piece hash
		if !bytes.Equal(hash.Sum(nil), task.pieceHash) {
			os.Remove(task.piecePath)
			goto StartDownloadPiece
		}

		wg.Done()
	}
}
