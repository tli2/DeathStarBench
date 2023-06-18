package test

import (
	"testing"
	"fmt"
	"github.com/stretchr/testify/assert"
	"socialnetworkk8/dialer"
	"context"
	userpb "socialnetworkk8/services/user/proto"
	graphpb "socialnetworkk8/services/graph/proto"
)

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
	assert.Equal(t, 1, len(res_get.Userids))
	assert.Equal(t, int64(2), res_get.Userids[0]) // user 1 has one followee user 2

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
	assert.Equal(t, 2, len(res_get.Userids))
	assert.Equal(t, int64(2), res_get.Userids[0]) // user 1 has two followees user 0 & 2
	assert.Equal(t, int64(0), res_get.Userids[1])

	// Unfollow
	arg_unfollow := graphpb.UnfollowRequest{Followerid: int64(1), Followeeid: int64(0)}
	res_unfollow, err := graphClient.Unfollow(context.Background(), &arg_unfollow)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_unfollow.Ok)

	res_get, err = graphClient.GetFollowers(context.Background(), &arg_get_flwER)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 0, len(res_get.Userids)) // user 0 has no follower
	
	res_get, err = graphClient.GetFollowees(context.Background(), &arg_get_flwEE)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 1, len(res_get.Userids))
	assert.Equal(t, int64(2), res_get.Userids[0]) // user 1 has one followee user 2

	// Stop fowarding
	assert.Nil(t, fcmd.Process.Kill())
}

func TestUserAndGraph(t *testing.T) {
	// start k8s port forwarding and set up client connection.
	testPortGraph := "9000"
	fcmdg, err := StartFowarding("graph", testPortGraph, "8085")
	assert.Nil(t, err)
	conn, err := dialer.Dial("localhost:" + testPortGraph, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	graphClient := graphpb.NewGraphClient(conn)
	assert.NotNil(t, graphClient)

	testPortUser := "9001"
	fcmdu, err := StartFowarding("user", testPortUser, "8084")
	assert.Nil(t, err)
	conn, err = dialer.Dial("localhost:" + testPortUser, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	userClient := userpb.NewUserClient(conn)
	assert.NotNil(t, userClient)

	// Create two users Alice and Bob 
	arg_reg1 := userpb.RegisterUserRequest{
		Firstname: "Alice", Lastname: "TUTest", Username: "atest", Password: "xyz"}
	arg_reg2 := userpb.RegisterUserRequest{
		Firstname: "Bob", Lastname: "TUTest", Username: "btest", Password: "zyx"}
	res_reg, err := userClient.RegisterUser(context.Background(), &arg_reg1)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_reg.Ok)
	auserid := res_reg.Userid
	res_reg, err = userClient.RegisterUser(context.Background(), &arg_reg2)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_reg.Ok)
	buserid := res_reg.Userid

	// Alice follows Bob
	arg_follow := graphpb.FollowWithUnameRequest{Followeruname: "atest", Followeeuname: "btest"}
	res_follow, err := graphClient.FollowWithUname(context.Background(), &arg_follow)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_follow.Ok)

	arg_get_flwER := graphpb.GetFollowersRequest{Followeeid: buserid}
	res_get, err := graphClient.GetFollowers(context.Background(), &arg_get_flwER)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 1, len(res_get.Userids))
	assert.Equal(t, auserid, res_get.Userids[0])

	arg_get_flwEE := graphpb.GetFolloweesRequest{Followerid: auserid}
	res_get, err = graphClient.GetFollowees(context.Background(), &arg_get_flwEE)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 1, len(res_get.Userids))
	assert.Equal(t, buserid, res_get.Userids[0])

	// Alice unfollows Bob
	arg_unfollow := graphpb.UnfollowWithUnameRequest{Followeruname: "atest", Followeeuname: "btest"}
	res_unfollow, err := graphClient.UnfollowWithUname(context.Background(), &arg_unfollow)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_unfollow.Ok)

	res_get, err = graphClient.GetFollowers(context.Background(), &arg_get_flwER)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 0, len(res_get.Userids))
	
	res_get, err = graphClient.GetFollowees(context.Background(), &arg_get_flwEE)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 0, len(res_get.Userids))

	// Stop fowarding
	assert.Nil(t, fcmdu.Process.Kill())
	assert.Nil(t, fcmdg.Process.Kill())
}
