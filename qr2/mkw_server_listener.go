package qr2

import (
	"encoding/binary"
	"fmt"
	"net"

	"wwfc/common"
	"wwfc/logging"
)

type ResponseFromMKWServer uint8

const (
	Ready       = 0
	PlayerAdded = 1
	Log         = 0xff
)

// Indicates that mkw-server is ready to accept tcp messages from wfc-server
type ReadyResponse struct {
	responseType ResponseFromMKWServer
	port         uint16 // used to identify itself
}

// Confirms that mkw-server added a player and is ready to communicate with them
type PlayerAddedResponse struct {
	responseType ResponseFromMKWServer
	searchId     uint64
}

// In TCP, messages can get cut off in a single send and be incomplete. But they can be
// completed in subsequent receives from mkw-server. We store each mkw-servers messages
// in a buffer to be able to complete them later
var mkwServerMessageBuffer map[string]*[]byte

// key is conn.RemoteAddr().String(), which is the only unique identifier in a net.Conn
// This is fine if wfc-server and mkw-server are on the same machine and listening to messages on localhost.
// But this is very dangerous for remote mkw-servers, since the address could be spoofed.
// So this map structure must be changed when remote mkw-server is implemented

func acceptMKWServerMessages() {
	mkwServerMessageBuffer = make(map[string]*[]byte)
	for {
		conn, err := mkwServerListener.Accept()
		if err != nil {
			logging.Error(moduleName, "Error accepting connection:", err)
			continue
		}
		go readMKWServerMessage(conn)
	}
}

func readMKWServerMessage(conn net.Conn) {
	defer conn.Close()

	buffer := make([]byte, 256)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			logging.Error(moduleName, "Error reading from connection:", err)
			return
		}
		msg := buffer[:n]
		handleMessageFromMKWServer(conn, msg)
	}
}

func handleMessageFromMKWServer(conn net.Conn, msg []byte) {
	if len(msg) == 0 {
		return
	}
	remoteAddr := conn.RemoteAddr()
	if remoteAddr == nil {
		logging.Info(moduleName, "conn.RemoteAddr() == nil")
		return
	}

	addr := remoteAddr.String()
	// add whatever we got to the message buffer
	buffer := addMsgToBuffer(addr, msg)
	if buffer == nil {
		logging.Info(moduleName, addr, "returned nil buffer")
		return
	}

	// handle as many complete messages we can
	for {
		// note that this updates buffer if we can handle a message
		completeMessage := popMessage(buffer)
		if completeMessage == nil {
			break
		}
		if len(completeMessage) == 0 {
			continue
		}
		handleCompleteMessage(completeMessage, conn)
	}
}

func handleCompleteMessage(completeMessage []byte, conn net.Conn) {
	var matchRequest ResponseFromMKWServer = ResponseFromMKWServer(completeMessage[0])
	switch matchRequest {
	case Ready:
		response, err := unpackReadyResponse(completeMessage)
		if err != nil {
			logging.Info(moduleName, err.Error())
			return
		}
		logging.Info(moduleName, "Handling MKWServerResponseType.Ready")
		mkwServer := mkwServers[int(response.port)]
		if mkwServer == nil {
			logging.Info(moduleName, "No MKWServer on port", response.port)
			return
		}
		room := mkwServer.roomPointer
		if room == nil {
			logging.Info(moduleName, "mkwServer.roomPointer is nil")
			return
		}
		if room.mkwServer == nil {
			logging.Info(moduleName, "Room", room.roomID, "mkwServer is nil")
			return
		}
		room.mkwServer.conn = conn
		err = room.sendMKWServerJoinRoomForEachPlayer()
		if err != nil {
			logging.Info(moduleName, err)
			return
		}
		logging.Info(moduleName, "Handled MKWServerResponseType.Ready!")

	case PlayerAdded:
		logging.Info(moduleName, "Handling MKWServerResponseType.PlayerAdded")
		response, err := unpackPlayerAddedResponse(completeMessage)
		if err != nil {
			logging.Info(moduleName, err.Error())
			return
		}
		if len(completeMessage) != 9 {
			logging.Info(moduleName, "Invalid MKWServerResponseType.PlayerAdded len:", len(completeMessage))
			return
		}
		searchId := response.searchId
		logging.Info(moduleName, "MKWServerResponseType.PlayerAdded searchId", searchId)
		player := playerBySearchID[searchId]
		if player == nil {
			logging.Info(moduleName, "Couldn't find player by search id! SearchId:", searchId, "while handling MKWServerResponseType.PlayerAdded")
			return
		}
		room := player.roomPointer
		if room == nil {
			logging.Info(moduleName, "Player", player.PlayerId, "isn't in a room! In MKWServerResponseType.PlayerAdded")
			return
		}
		mkwServer := room.mkwServer
		if mkwServer == nil {
			logging.Info(moduleName, "Room", room.roomID, "has nil mkwServer!")
			return
		}
		common.SendPacket(ServerName, player.connIdx, packMKWServerAddressPacket(mkwServer.udpAddr))

	case Log:
		logging.Notice("MKW-Server Log", string(completeMessage[1:]))

	default:
		logging.Info(moduleName, "invalid matchRequest:", matchRequest)
	}
}

func unpackReadyResponse(completeMessage []byte) (ReadyResponse, error) {
	if len(completeMessage) != 3 {
		return ReadyResponse{}, fmt.Errorf("invalid ReadyResponse length: %d", len(completeMessage))
	}
	return ReadyResponse{
		responseType: ResponseFromMKWServer(completeMessage[0]),
		port:         binary.BigEndian.Uint16(completeMessage[1:]),
	}, nil
}

func unpackPlayerAddedResponse(completeMessage []byte) (PlayerAddedResponse, error) {
	if len(completeMessage) != 9 {
		return PlayerAddedResponse{}, fmt.Errorf("invalid PlayerAddedResponse length: %d", len(completeMessage))
	}
	return PlayerAddedResponse{
		responseType: ResponseFromMKWServer(completeMessage[0]),
		searchId:     binary.BigEndian.Uint64(completeMessage[1:]),
	}, nil
}
