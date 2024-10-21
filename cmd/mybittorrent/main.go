package main

import (
	"encoding/json"
	"fmt"
	"io"
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

	taskCh := make(chan task)
	wg := sync.WaitGroup{}
	go downloadPiece(torrent, peers[0], taskCh, &wg)
	wg.Add(1)
	taskCh <- task{piecePath: piecePath, pieceIndex: pieceIndex}

	wg.Wait()
	close(taskCh)
}

func cmdDownload() {
	piecePath := os.Args[3]

	torrent, err := parseTorrentFile(os.Args[4])
	if err != nil {
		panic(err)
	}

	peers, err := getPeers(torrent)
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
	for i := 0; i < pieceCount; i++ {
		taskCh <- task{piecePath: fmt.Sprintf("%s-%d", piecePath, i), pieceIndex: i}
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
	default:
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
