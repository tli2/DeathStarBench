package test

import (
	"testing"
	"fmt"
	"github.com/stretchr/testify/assert"
	"socialnetworkk8/dialer"
	"context"
	postpb "socialnetworkk8/services/post/proto"
	tlpb "socialnetworkk8/services/timeline/proto"

)

func IsPostEqual(a, b *postpb.Post) bool {
	if a.Postid != b.Postid || a.Posttype != b.Posttype || 
			a.Timestamp != b.Timestamp || a.Text != b.Text || 
			a.Creator != b.Creator || len(a.Medias) != len(a.Medias) || 
			len(a.Usermentions) != len(b.Usermentions) || len(a.Urls) != len(b.Urls) {
		return false
	} 
	for idx, _ := range a.Usermentions {
		if a.Usermentions[idx] !=  b.Usermentions[idx] {
			return false
		}
	}
	for idx, _ := range a.Urls {
		if a.Urls[idx] != b.Urls[idx] {
			return false
		}
	}
	for idx, _ := range a.Medias {
		if a.Medias[idx] != b.Medias[idx] {
			return false
		}
	}
	return true
}

func createNPosts(t *testing.T, postc postpb.PostStorageClient, N int, userid int64) []*postpb.Post {
	posts := make([]*postpb.Post, N)
	for i := 0; i < N; i++ {
		posts[i] = &postpb.Post{
			Postid: int64(i),
			Posttype: postpb.POST_TYPE_POST,
			Timestamp: int64(10000+i),
			Creator: userid,
			Text: fmt.Sprintf("Post Number %v", i+1),
			Urls: []string{"xxxxx"},
			Usermentions: []int64{userid*10+int64(i+1)},
		}
		arg_store := &postpb.StorePostRequest{Post: posts[i]}
		res_store, err := postc.StorePost(context.Background(), arg_store)
		assert.Nil(t, err)
		assert.Equal(t, "OK", res_store.Ok)
	}
	return posts
} 

func writeTimeline(t *testing.T, tlc tlpb.TimelineClient, post *postpb.Post, userid int64) {
	arg_write := &tlpb.WriteTimelineRequest{
		Userid: userid, 
		Postid: post.Postid, 
		Timestamp: post.Timestamp}
	res_write, err := tlc.WriteTimeline(context.Background(), arg_write)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_write.Ok)
}

func TestPost(t *testing.T) {
	// start k8s port forwarding and set up client connection.
	testPort := "9000"
	fcmd, err := StartFowarding("post", testPort, "8086")
	assert.Nil(t, err)
	conn, err := dialer.Dial("localhost:" + testPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	postClient := postpb.NewPostStorageClient(conn)
	assert.NotNil(t, postClient)

	// create two posts
	post1 := postpb.Post{
		Postid: int64(1),
		Posttype: postpb.POST_TYPE_POST,
		Timestamp: int64(12345),
		Creator: int64(200),
		Text: "First Post",
		Usermentions: []int64{int64(201)},
		Medias: []int64{int64(777)},
		Urls: []string{"XXXXX"},
	}
	post2 := postpb.Post{
		Postid: int64(2),
		Posttype: postpb.POST_TYPE_REPOST,
		Timestamp: int64(67890),
		Creator: int64(200),
		Text: "Second Post",
		Usermentions: []int64{int64(202)},
		Urls: []string{"YYYYY"},
	}

	// store first post
	arg_store := &postpb.StorePostRequest{Post: &post1}
	res_store, err := postClient.StorePost(context.Background(), arg_store) 
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_store.Ok)
	
	// check for two posts. one missing
	arg_read := &postpb.ReadPostsRequest{Postids: []int64{int64(1), int64(2)}}
	res_read, err := postClient.ReadPosts(context.Background(), arg_read) 
	assert.Nil(t, err)
	assert.Equal(t, "No. Missing 2.", res_read.Ok)
	
	// store second post and check for both.
	arg_store.Post = &post2
	res_store, err = postClient.StorePost(context.Background(), arg_store) 
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_store.Ok)
	
	res_read, err = postClient.ReadPosts(context.Background(), arg_read) 
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_read.Ok)
	assert.True(t, IsPostEqual(&post1, res_read.Posts[0]))
	assert.True(t, IsPostEqual(&post2, res_read.Posts[1]))

	// Stop forwarding
	assert.Nil(t, fcmd.Process.Kill())
}

func TestTimeline(t *testing.T) {
	// start forwarding
	postTestPort, tlTestPort := "9000", "9001"
	pfcmd, err := StartFowarding("post", postTestPort, "8086")
	assert.Nil(t, err)
	tfcmd, err := StartFowarding("timeline", tlTestPort, "8089")
	assert.Nil(t, err)
	postConn, err := dialer.Dial("localhost:" + postTestPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	postClient := postpb.NewPostStorageClient(postConn)
	assert.NotNil(t, postClient)
	tlConn, err := dialer.Dial("localhost:" + tlTestPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	tlClient := tlpb.NewTimelineClient(tlConn)
	assert.NotNil(t, tlClient)

	// create and store N posts
	NPOST, userid := 4, int64(200)
	posts := createNPosts(t, postClient, NPOST, userid)

	// write posts 0 to N/2 to timeline
	for i := 0; i < NPOST/2; i++ {
		writeTimeline(t, tlClient, posts[i], userid) 
	}
	arg_read := &tlpb.ReadTimelineRequest{Userid: userid, Start: int32(0), Stop: int32(1)}
	res_read, err := tlClient.ReadTimeline(context.Background(), arg_read)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(res_read.Posts))
	assert.Equal(t, "OK", res_read.Ok)
	assert.True(t, IsPostEqual(posts[NPOST/2-1], res_read.Posts[0]))
	arg_read.Stop = int32(NPOST)
	res_read, err = tlClient.ReadTimeline(context.Background(), arg_read)
	assert.Nil(t, err)
	assert.Equal(t, 
		fmt.Sprintf("Cannot process start=0 end=%v for %v items", NPOST, NPOST/2), res_read.Ok) 

	// write post N/2 to N to timeline 	
	for i := NPOST/2; i < NPOST; i++ {
		writeTimeline(t, tlClient, posts[i], userid) 
	}
	arg_read.Start = int32(1)
	res_read, err = tlClient.ReadTimeline(context.Background(), arg_read)
	assert.Nil(t, err)
	assert.Equal(t, NPOST-1, len(res_read.Posts))
	assert.Equal(t, "OK", res_read.Ok)
	for i, tlpost := range(res_read.Posts) {
		// posts should be in reverse order
		assert.True(t, IsPostEqual(posts[NPOST-i-2], tlpost))
	}

	// Stop forwarding
	assert.Nil(t, pfcmd.Process.Kill())
	assert.Nil(t, tfcmd.Process.Kill())
}
