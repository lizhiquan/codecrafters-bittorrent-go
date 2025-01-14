package main

import (
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
	fmt.Printf("Info Hash: %x\n", torrent.Info.Hash())
	fmt.Printf("Piece Length: %d\n", torrent.Info.PieceLength)
	fmt.Println("Piece Hashes:")
	for _, hash := range torrent.Info.PieceHashes() {
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
		InfoHash: torrent.Info.Hash(),
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

	conn, _, err := dialPeer(peers[0], torrent.Info.Hash(), false)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	taskCh := make(chan task)
	wg := sync.WaitGroup{}
	go downloadPiece(conn, &torrent.Info, taskCh, &wg)
	wg.Add(1)
	taskCh <- task{
		piecePath:  piecePath,
		pieceIndex: pieceIndex,
		pieceHash:  torrent.Info.PieceHashes()[pieceIndex],
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
		conn, _, err := dialPeer(peers[i], torrent.Info.Hash(), false)
		if err != nil {
			panic(err)
		}
		defer conn.Close()

		go downloadPiece(conn, &torrent.Info, taskCh, &wg)
	}

	size := torrent.Info.Length
	pieceSize := torrent.Info.PieceLength
	pieceCount := int(math.Ceil(float64(size) / float64(pieceSize)))
	wg.Add(pieceCount)
	pieceHashes := torrent.Info.PieceHashes()
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
	handshake.SetExtension()
	if err := marshalHandshakeMessage(conn, &handshake); err != nil {
		panic(err)
	}

	if err := unmarshalHandshakeMessage(conn, &handshake); err != nil {
		panic(err)
	}

	fmt.Printf("Peer ID: %x\n", handshake.PeerID)

	if !handshake.IsExtension() {
		log.Println("extension not supported")
		return
	}

	// read bitfield
	var m PeerMessage
	if err := unmarshalPeerMessage(conn, &m); err != nil {
		panic(err)
	}
	if m.ID != IDBitfield {
		panic("expect bitfield")
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
		panic(err)
	}
	m = PeerMessage{
		ID:      IDExtension,
		Payload: payload,
	}
	if err := marshalPeerMessage(conn, &m); err != nil {
		panic(err)
	}
	if err := unmarshalPeerMessage(conn, &m); err != nil {
		panic(err)
	}
	if m.ID != IDExtension {
		panic("expect extension")
	}

	if err := extensionPayload.UnmarshalBinary(m.Payload); err != nil {
		panic(err)
	}

	peerExtID := extensionPayload.Message.(map[string]any)["m"].(map[string]any)["ut_metadata"]
	fmt.Printf("Peer Metadata Extension ID: %v\n", peerExtID)
}

func cmdMagnetInfo() {
	magnetURL := os.Args[2]

	magnet, err := NewMagnet(magnetURL)
	if err != nil {
		panic(err)
	}

	peers, err := magnet.Peers()
	if err != nil {
		panic(err)
	}

	if len(peers) == 0 {
		panic("no peers")
	}

	conn, torrentInfo, err := dialPeer(peers[0], magnet.InfoHash, true)
	if err != nil {
		panic(err)
	}
	conn.Close()

	fmt.Printf("Tracker URL: %s\n", magnet.TrackerURL)
	fmt.Printf("Length: %d\n", torrentInfo.Length)
	fmt.Printf("Info Hash: %x\n", torrentInfo.Hash())
	fmt.Printf("Piece Length: %d\n", torrentInfo.PieceLength)
	fmt.Println("Piece Hashes:")
	for _, hash := range torrentInfo.PieceHashes() {
		fmt.Printf("%x\n", hash)
	}
}

func cmdMagnetDownloadPiece() {
	piecePath := os.Args[3]

	magnet, err := NewMagnet(os.Args[4])
	if err != nil {
		panic(err)
	}

	pieceIndex, err := strconv.Atoi(os.Args[5])
	if err != nil {
		panic(err)
	}

	peers, err := magnet.Peers()
	if err != nil {
		panic(err)
	}
	if len(peers) == 0 {
		panic("no peers")
	}

	conn, torrentInfo, err := dialPeer(peers[0], magnet.InfoHash, true)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	taskCh := make(chan task)
	wg := sync.WaitGroup{}
	go downloadPiece(conn, torrentInfo, taskCh, &wg)
	wg.Add(1)
	taskCh <- task{
		piecePath:  piecePath,
		pieceIndex: pieceIndex,
		pieceHash:  torrentInfo.PieceHashes()[pieceIndex],
	}

	wg.Wait()
	close(taskCh)
}

func cmdMagnetDownload() {
	piecePath := os.Args[3]

	magnet, err := NewMagnet(os.Args[4])
	if err != nil {
		panic(err)
	}

	peers, err := magnet.Peers()
	if err != nil {
		panic(err)
	}

	taskCh := make(chan task)
	wg := sync.WaitGroup{}
	var torrentInfo *TorrentInfo
	for i := 0; i < len(peers); i++ {
		var conn net.Conn
		conn, torrentInfo, err = dialPeer(peers[i], magnet.InfoHash, true)
		if err != nil {
			panic(err)
		}
		defer conn.Close()

		go downloadPiece(conn, torrentInfo, taskCh, &wg)
	}

	size := torrentInfo.Length
	pieceSize := torrentInfo.PieceLength
	pieceCount := int(math.Ceil(float64(size) / float64(pieceSize)))
	wg.Add(pieceCount)
	pieceHashes := torrentInfo.PieceHashes()
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
	case "magnet_info":
		cmdMagnetInfo()
	case "magnet_download_piece":
		cmdMagnetDownloadPiece()
	case "magnet_download":
		cmdMagnetDownload()
	default:
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
