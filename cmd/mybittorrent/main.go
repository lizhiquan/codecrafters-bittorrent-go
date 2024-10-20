package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"

	bencode "github.com/jackpal/bencode-go"
)

func cmdDecode() {
	bencodedValue := os.Args[2]

	decoded, err := bencode.Decode(strings.NewReader(bencodedValue))
	if err != nil {
		panic(err)
	}

	jsonOutput, _ := json.Marshal(decoded)
	fmt.Println(string(jsonOutput))
}

func cmdInfo() {
	torrent, err := parseTorrentFile(os.Args[2])
	if err != nil {
		panic(err)
	}

	fmt.Printf("Tracker URL: %s\n", torrent.Announce)
	fmt.Printf("Length: %d\n", torrent.Info.Length)
	fmt.Printf("Info Hash: %x\n", torrent.InfoHash())
	fmt.Printf("Piece Length: %d\n", torrent.Info.PieceLength)
	fmt.Println("Piece Hashes:")
	for _, hash := range torrent.PieceHashes() {
		fmt.Println(hash)
	}
}

func cmdPeers() {
	torrent, err := parseTorrentFile(os.Args[2])
	if err != nil {
		panic(err)
	}

	peers, err := getPeers(torrent)
	if err != nil {
		panic(err)
	}

	for _, peer := range peers {
		fmt.Println(peer)
	}
}

func cmdHandshake() {
	torrent, err := parseTorrentFile(os.Args[2])
	if err != nil {
		panic(err)
	}

	peerAddr := os.Args[3]
	conn, err := net.Dial("tcp", peerAddr)
	if err != nil {
		panic(err)
	}

	defer conn.Close()

	m := HandshakeMessage{
		Protocol: "BitTorrent protocol",
		InfoHash: torrent.InfoHash(),
		PeerID:   peerID(),
	}
	if err := marshalHandshakeMessage(conn, &m); err != nil {
		panic(err)
	}

	var response HandshakeMessage
	if err := unmarshalHandshakeMessage(conn, &response); err != nil {
		panic(err)
	}

	fmt.Printf("Peer ID: %x\n", response.PeerID)
}

func cmdDownloadPiece() {
	piecePath := os.Args[3]

	torrent, err := parseTorrentFile(os.Args[4])
	if err != nil {
		panic(err)
	}

	pieceIndex, err := strconv.Atoi(os.Args[5])
	if err != nil {
		panic(err)
	}

	peers, err := getPeers(torrent)
	if err != nil {
		panic(err)
	}
	if len(peers) == 0 {
		panic("no peers")
	}

	conn, err := net.Dial("tcp", peers[0])
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

	// create piece file
	pieceFile, err := os.Create(piecePath)
	if err != nil {
		panic(err)
	}
	defer pieceFile.Close()

	// download piece
	size := torrent.Info.Length
	pieceSize := torrent.Info.PieceLength
	pieceCount := int(math.Ceil(float64(size) / float64(pieceSize)))
	if pieceIndex == pieceCount-1 {
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
			Index:  uint32(pieceIndex),
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
	}
}

func main() {
	command := os.Args[1]

	switch command {
	case "decode":
		cmdDecode()
	case "info":
		cmdInfo()
	case "peers":
		cmdPeers()
	case "handshake":
		cmdHandshake()
	case "download_piece":
		cmdDownloadPiece()
	default:
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
