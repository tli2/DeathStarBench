package test

import (
	"testing"
	"fmt"
	"github.com/stretchr/testify/assert"
	"socialnetworkk8/dialer"
	"context"
	postpb "socialnetworkk8/services/post/proto"
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

	// Stop fowarding
	assert.Nil(t, fcmd.Process.Kill())
}
