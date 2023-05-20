package socialnetworkk8_test

import (
	"testing"
	"fmt"
	"github.com/stretchr/testify/assert"
	"socialnetworkk8/dialer"
	"context"
	geo "socialnetworkk8/services/geo/proto"
	"os/exec"
	"time"
)




func StartFowarding(service, testPort, targetPort string) (*exec.Cmd, error) {
	cmd := exec.Command("kubectl", "port-forward", "svc/"+service, testPort+":"+targetPort)
	if err := cmd.Start(); err != nil {
		return nil,  err
	}
	time.Sleep(1*time.Second)
	return cmd, nil
}

func TestGeo(t *testing.T) {
	testPort := "9000"
	fcmd, err := StartFowarding("geo", testPort, "8083")
	assert.Nil(t, err)
	conn, err := dialer.Dial("localhost:" + testPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	geoClient := geo.NewGeoClient(conn)
	assert.NotNil(t, geoClient)
	res, err := geoClient.Nearby(context.Background(), &geo.Request{Lat: 37.7936, Lon: -122.393})
	assert.Nil(t, err)
	assert.Equal(t, 5, len(res.HotelIds))
	assert.Nil(t, fcmd.Process.Kill())
}
