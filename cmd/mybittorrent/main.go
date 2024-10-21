package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

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
	torrent, err := NewTorrent(os.Args[2])
	if err != nil {
		panic(err)
	}

	fmt.Printf("Tracker URL: %s\n", torrent.Announce)
	fmt.Printf("Length: %d\n", torrent.Info.Length)
	fmt.Printf("Info Hash: %x\n", torrent.InfoHash())
	fmt.Printf("Piece Length: %d\n", torrent.Info.PieceLength)
	fmt.Println("Piece Hashes:")
	for _, hash := range torrent.PieceHashes() {
		fmt.Printf("%x\n", hash)
	}
}

func cmdPeers() {
	torrent, err := NewTorrent(os.Args[2])
	if err != nil {
		panic(err)
	}

	peers, err := torrent.Peers()
	if err != nil {
		panic(err)
	}

	for _, peer := range peers {
		fmt.Println(peer)
	}
}

func cmdHandshake() {
	torrent, err := NewTorrent(os.Args[2])
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

	torrent, err := NewTorrent(os.Args[4])
	if err != nil {
		panic(err)
	}

	pieceIndex, err := strconv.Atoi(os.Args[5])
	if err != nil {
		panic(err)
	}

	peers, err := torrent.Peers()
	if err != nil {
		panic(err)
	}
	if len(peers) == 0 {
		panic("no peers")
	}

	taskCh := make(chan task)
	wg := sync.WaitGroup{}
	go downloadPiece(torrent, peers[0], taskCh, &wg)
	wg.Add(1)
	taskCh <- task{
		piecePath:  piecePath,
		pieceIndex: pieceIndex,
		pieceHash:  torrent.PieceHashes()[pieceIndex],
	}

	wg.Wait()
	close(taskCh)
}

func cmdDownload() {
	piecePath := os.Args[3]

	torrent, err := NewTorrent(os.Args[4])
	if err != nil {
		panic(err)
	}

	peers, err := torrent.Peers()
	if err != nil {
		panic(err)
	}

	taskCh := make(chan task)
	wg := sync.WaitGroup{}
	for i := 0; i < len(peers); i++ {
		go downloadPiece(torrent, peers[i], taskCh, &wg)
	}

	size := torrent.Info.Length
	pieceSize := torrent.Info.PieceLength
	pieceCount := int(math.Ceil(float64(size) / float64(pieceSize)))
	wg.Add(pieceCount)
	pieceHashes := torrent.PieceHashes()
	for i := 0; i < pieceCount; i++ {
		taskCh <- task{
			piecePath:  fmt.Sprintf("%s-%d", piecePath, i),
			pieceIndex: i,
			pieceHash:  pieceHashes[i],
		}
	}

	wg.Wait()
	close(taskCh)

	// merge pieces to target file
	targetFile, err := os.Create(piecePath)
	if err != nil {
		panic(err)
	}
	defer targetFile.Close()

	for i := 0; i < pieceCount; i++ {
		pieceFile, err := os.Open(fmt.Sprintf("%s-%d", piecePath, i))
		if err != nil {
			panic(err)
		}

		if _, err := io.Copy(targetFile, pieceFile); err != nil {
			panic(err)
		}

		pieceFile.Close()
	}

	for i := 0; i < pieceCount; i++ {
		os.Remove(fmt.Sprintf("%s-%d", piecePath, i))
	}
}

func cmdMagnetParse() {
	magnetURL := os.Args[2]

	m, err := NewMagnet(magnetURL)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Tracker URL: %s\n", m.TrackerURL)
	fmt.Printf("Info Hash: %x\n", m.InfoHash)
}

func cmdMagnetHandshake() {
	magnetURL := os.Args[2]

	magnet, err := NewMagnet(magnetURL)
	if err != nil {
		panic(err)
	}

	peers, err := magnet.Peers()
	if err != nil {
		panic(err)
	}

	fmt.Println(peers)
	if len(peers) == 0 {
		panic("no peers")
	}

	conn, err := net.Dial("tcp", peers[0])
	if err != nil {
		panic(err)
	}

	defer conn.Close()

	handshake := HandshakeMessage{
		Protocol: "BitTorrent protocol",
		InfoHash: magnet.InfoHash,
		PeerID:   peerID(),
	}
	// https://www.bittorrent.org/beps/bep_0010.html
	handshake.Reserved[5] = 1 << 4
	if err := marshalHandshakeMessage(conn, &handshake); err != nil {
		panic(err)
	}

	if err := unmarshalHandshakeMessage(conn, &handshake); err != nil {
		panic(err)
	}

	fmt.Printf("Peer ID: %x\n", handshake.PeerID)

	if handshake.Reserved[5]&(1<<4) == 0 {
		log.Println("extension not supported")
		return
	}

	// extension handshake
	mID := byte(0)
	var buffer bytes.Buffer
	buffer.WriteByte(mID)
	if err := bencode.Marshal(&buffer, map[string]any{
		"m": map[string]any{
			"ut_metadata": 1,
		},
	}); err != nil {
		panic(err)
	}
	m := PeerMessage{
		ID:      20,
		Payload: buffer.Bytes(),
	}
	if err := marshalPeerMessage(conn, &m); err != nil {
		panic(err)
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
	case "download":
		cmdDownload()
	case "magnet_parse":
		cmdMagnetParse()
	case "magnet_handshake":
		cmdMagnetHandshake()
	default:
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
