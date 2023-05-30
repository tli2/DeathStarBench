package socialnetworkk8_test

import (
	"testing"
	"fmt"
	"github.com/stretchr/testify/assert"
	"socialnetworkk8/dialer"
	"context"
	geo "socialnetworkk8/services/geo/proto"
	userpb "socialnetworkk8/services/user/proto"
	graphpb "socialnetworkk8/services/graph/proto"
)

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

func TestUser(t *testing.T) {
	// start k8s port forwarding and set up client connection.
	testPort := "9000"
	fcmd, err := StartFowarding("user", testPort, "8084")
	assert.Nil(t, err)
	conn, err := dialer.Dial("localhost:" + testPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	userClient := userpb.NewUserClient(conn)
	assert.NotNil(t, userClient)

	// check user
	arg_check := &userpb.CheckUserRequest{Usernames: []string{"test_user"}}
	res_check, err := userClient.CheckUser(context.Background(), arg_check)
	assert.Nil(t, err)
	assert.Equal(t, "No", res_check.Ok)
	assert.Equal(t, int64(-1), res_check.Userids[0])

	// register user
	arg_reg := &userpb.RegisterUserRequest{
		Firstname: "Alice", Lastname: "Test", Username: "user_0", Password: "xxyyzz"}
	res_reg, err := userClient.RegisterUser(context.Background(), arg_reg)
	assert.Nil(t, err)
	assert.Equal(t, "Username user_0 already exist", res_reg.Ok)

	arg_reg.Username = "test_user"
	res_reg, err = userClient.RegisterUser(context.Background(), arg_reg)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_reg.Ok)
	created_userid := res_reg.Userid

	// check user
	arg_check.Usernames = []string{"test_user", "user_1", "user_2"}
	res_check, err = userClient.CheckUser(context.Background(), arg_check)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_check.Ok)
	assert.Equal(t, created_userid, res_check.Userids[0])
	assert.Equal(t, int64(1), res_check.Userids[1])
	assert.Equal(t, int64(2), res_check.Userids[2])

	// new user login
	arg_login := &userpb.LoginRequest{Username: "test_user", Password: "xxyy"}
	res_login, err := userClient.Login(context.Background(), arg_login)
	assert.Nil(t, err)
	assert.Equal(t, "Login Failure.", res_login.Ok)

	arg_login.Password = "xxyyzz"
	res_login, err = userClient.Login(context.Background(), arg_login)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_login.Ok)

	// Stop fowarding
	assert.Nil(t, fcmd.Process.Kill())
}

func TestGraph(t *testing.T) {
	// start k8s port forwarding and set up client connection.
	testPort := "9000"
	fcmd, err := StartFowarding("graph", testPort, "8085")
	assert.Nil(t, err)
	conn, err := dialer.Dial("localhost:" + testPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	graphClient := graphpb.NewGraphClient(conn)
	assert.NotNil(t, graphClient)

	// get follower and followee list
	arg_get_flwER := graphpb.GetFollowersRequest{Followeeid: int64(0)}
	res_get, err := graphClient.GetFollowers(context.Background(), &arg_get_flwER)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 0, len(res_get.Userids)) // user 0 has no follower
	
	arg_get_flwEE := graphpb.GetFolloweesRequest{Followerid: int64(1)}
	res_get, err = graphClient.GetFollowees(context.Background(), &arg_get_flwEE)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 0, len(res_get.Userids))
	//assert.Equal(t, int64(2), res_get.Userids[0]) // user 1 has one followee user 2

	// Follow
	arg_follow := graphpb.FollowRequest{Followerid: int64(1), Followeeid: int64(0)}
	res_follow, err := graphClient.Follow(context.Background(), &arg_follow)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_follow.Ok)

	res_get, err = graphClient.GetFollowers(context.Background(), &arg_get_flwER)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 1, len(res_get.Userids))
	assert.Equal(t, int64(1), res_get.Userids[0]) // user 0 has one follower user 1

	res_get, err = graphClient.GetFollowees(context.Background(), &arg_get_flwEE)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 1, len(res_get.Userids))
	assert.Equal(t, int64(0), res_get.Userids[0]) // user 1 has two followees user 0 & 2
	//assert.Equal(t, int64(2), res_get.Userids[1]) // user 1 has two followees user 0 & 2

	// Stop fowarding
	assert.Nil(t, fcmd.Process.Kill())

}
