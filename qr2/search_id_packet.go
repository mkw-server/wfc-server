package qr2

import (
	"encoding/binary"
	"net"

	"wwfc/common"
	"wwfc/logging"

	"github.com/logrusorgru/aurora/v3"
)

// See the comment in the client's SearchIDPacket struct for purpose
type SearchIdPacket struct {
	Magic    [8]uint8 // magic, always "SEARCHID"
	SearchId uint64   // search ID of the player
}

// check the SearchIDPacket magic and size, then verify if the id is actually correct
func tryVerifySearchIdPacketReceipt(addr net.UDPAddr, data []byte) bool {
	if len(data) != 16 {
		return false
	}

	if string(data[:8]) != "SEARCHID" {
		return false
	}

	player := players[common.MakeLookupAddr(addr.String())]
	if player == nil {
		return false
	}

	searchID := binary.BigEndian.Uint64(data[8:16])
	if player.SearchId == searchID {
		logging.Info(moduleName, "Received SearchIdPacket from player", player.PlayerId, "with matching search ID", aurora.Cyan(searchID))
		player.recvSearchId = true
		return true
	} else {
		player.searchIdGuesses++
		logging.Info(moduleName, "Received SearchIdPacket from", player.PlayerId, "with non-matching search ID", aurora.Cyan(searchID), "expected", aurora.Cyan(player.SearchId))
		return false
	}
}
