package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"

	bencode "github.com/jackpal/bencode-go"
)

func peerID() []byte {
	bytes := make([]byte, 20)
	_, err := rand.Read(bytes)
	if err != nil {
		panic(err)
	}
	return bytes
}

func getPeers(trackerURL string, infoHash []byte, length int) ([]string, error) {
	req, err := http.NewRequest("GET", trackerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	query := req.URL.Query()
	query.Add("info_hash", string(infoHash))
	query.Add("peer_id", string(peerID()))
	query.Add("port", "6881")
	query.Add("uploaded", "0")
	query.Add("downloaded", "0")
	query.Add("left", strconv.Itoa(length))
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
	StartDownloadPiece:
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
