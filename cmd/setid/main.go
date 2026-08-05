// Command setid sets a motor ID connected to the serial bus.
package setid

import (
	"flag"
	"time"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/robot2/motors"
)

func main() {
	var oldID int
	var newID int
	var motorPort string
	flag.StringVar(&motorPort, "motor-port", "", "path to motorbus serial port")
	flag.IntVar(&oldID, "old-id", 1, "old ID to replace")
	flag.IntVar(&newID, "new-id", -1, "new ID value")
	flag.Parse()
	if newID == -1 {
		essentials.Die("must specify -new-id")
	}
	motorConn, err := motors.NewConnection(motorPort, time.Second*2)
	essentials.Must(err)
	essentials.Must(motorConn.SetID(uint8(oldID), uint8(newID)))
}
