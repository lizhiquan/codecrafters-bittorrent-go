package main

import (
	"crypto/rand"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"

	bencode "github.com/jackpal/bencode-go"
)

func parseTorrentFile(path string) (*Torrent, error) {
	torrentFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer torrentFile.Close()

	var torrent Torrent
	err = bencode.Unmarshal(torrentFile, &torrent)
	if err != nil {
		return nil, err
	}

	return &torrent, nil
}

func peerID() []byte {
	bytes := make([]byte, 20)
	_, err := rand.Read(bytes)
	if err != nil {
		panic(err)
	}
	return bytes
}

func getPeers(torrent *Torrent) ([]string, error) {
	req, err := http.NewRequest("GET", torrent.Announce, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	query := req.URL.Query()
	query.Add("info_hash", string(torrent.InfoHash()))
	query.Add("peer_id", string(peerID()))
	query.Add("port", "6881")
	query.Add("uploaded", "0")
	query.Add("downloaded", "0")
	query.Add("left", strconv.Itoa(torrent.Info.Length))
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
}

func downloadPiece(torrent *Torrent, peerAddr string, taskCh chan task, wg *sync.WaitGroup) {
	conn, err := net.Dial("tcp", peerAddr)
	if err != nil {
		panic(err)
	}

	defer conn.Close()

	handshakeMessage := HandshakeMessage{
		Protocol: "BitTorrent protocol",
		InfoHash: torrent.InfoHash(),
		PeerID:   peerID(),
	}
	if err := marshalHandshakeMessage(conn, &handshakeMessage); err != nil {
		panic(err)
	}
	if err := unmarshalHandshakeMessage(conn, &handshakeMessage); err != nil {
		panic(err)
	}

	// read bitfield
	var m PeerMessage
	if err := unmarshalPeerMessage(conn, &m); err != nil {
		panic(err)
	}

	if m.ID != IDBitfield {
		panic("expect bitfield")
	}

	// send interested
	m = PeerMessage{ID: IDInterested}
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
		// create piece file
		pieceFile, err := os.Create(task.piecePath)
		if err != nil {
			panic(err)
		}

		// download piece
		size := torrent.Info.Length
		pieceSize := torrent.Info.PieceLength
		pieceCount := int(math.Ceil(float64(size) / float64(pieceSize)))
		if task.pieceIndex == pieceCount-1 {
			pieceSize = size % pieceSize
		}
		blockSize := 16 * 1024 // 16KB
		blockCount := int(math.Ceil(float64(pieceSize) / float64(blockSize)))
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

			// write piece to file
			if _, err := pieceFile.Write(piecePayload.Block); err != nil {
				panic(err)
			}

			// TODO: checksum
			// if checksum is not correct, delete the piece file and retry
		}

		pieceFile.Close()
		wg.Done()
	}
}
