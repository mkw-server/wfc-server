package qr2

import (
	"fmt"

	"wwfc/common"
	"wwfc/logging"
)

func findPublicRoom(p *Player, region common.MKWServerSearchRegion, gameMode common.MKWServerGameMode) error {
	for id, r := range rooms {
		if id == "" || r == nil {
			continue
		}

		if r.Region == common.Private {
			continue
		}

		// check that the rooms region is the same as the requested region
		if r.Region != region {
			continue
		}

		if r.gameMode != gameMode {
			continue
		}

		logging.Info(moduleName, "Found a public room", r.roomID, "Player", p.PlayerId, "can join!")

		// to support vr based searches, we would need to loop through all rooms before adding players
		err := r.tryAddPlayer(p, false)
		if err != nil {
			logging.Info(moduleName, "findPublicRoom(): %s", err.Error(), "Continuing room search")
			continue
		} else {
			return nil
		}
	}

	// if we reach here, then there are no available rooms, so create a public room with the specified region
	logging.Info(moduleName, "No rooms specify search criteria, creating a room for Player", p.PlayerId)
	err := createRoom(p, region, gameMode)
	if err != nil {
		return fmt.Errorf("Player %d failed to create a public room. createRoom() returned %s", p.PlayerId, err.Error())
	}
	return nil

}
