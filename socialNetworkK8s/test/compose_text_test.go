package test

import (
	"testing"
	"fmt"
	"github.com/stretchr/testify/assert"
	"socialnetworkk8/dialer"
	"context"
	"strings"
	urlpb "socialnetworkk8/services/url/proto"
	textpb "socialnetworkk8/services/text/proto"
	composepb "socialnetworkk8/services/compose/proto"
	tlpb "socialnetworkk8/services/timeline/proto"
	homepb "socialnetworkk8/services/home/proto"
	postpb "socialnetworkk8/services/post/proto"
)

func TestUrl(t *testing.T) {
	// start server
	testPort := "9000"
	fcmd, err := StartFowarding("url", testPort, "8087")
	assert.Nil(t, err)
	conn, err := dialer.Dial("localhost:" + testPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	urlClient := urlpb.NewUrlClient(conn)
	assert.NotNil(t, urlClient)

	// compose urls
	url1 := "http://www.google.com/q=apple"
	url2 := "https://www.bing.com"
	arg_url := &urlpb.ComposeUrlsRequest{Extendedurls: []string{url1, url2}}
	res_url, err := urlClient.ComposeUrls(context.Background(), arg_url)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_url.Ok)
	assert.Equal(t, 2, len(res_url.Shorturls))

	// get urls
	shortUrl1 := res_url.Shorturls[0]
	shortUrl2 := res_url.Shorturls[1]
	arg_get := &urlpb.GetUrlsRequest{Shorturls: []string{shortUrl1, shortUrl2}}
	res_get, err := urlClient.GetUrls(context.Background(), arg_get)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 2, len(res_get.Extendedurls))
	assert.Equal(t, url1, res_get.Extendedurls[0])
	assert.Equal(t, url2, res_get.Extendedurls[1])

	// Stop fowarding
	assert.Nil(t, fcmd.Process.Kill())
}

func TestText(t *testing.T) {
	// start server
	testPort := "9000"
	fcmd, err := StartFowarding("text", testPort, "8088")
	assert.Nil(t, err)
	conn, err := dialer.Dial("localhost:" + testPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	textClient := textpb.NewTextClient(conn)
	assert.NotNil(t, textClient)

	// process text
	arg_text := &textpb.ProcessTextRequest{}
	res_text, err := textClient.ProcessText(context.Background(), arg_text)
	assert.Nil(t, err)
	assert.Equal(t, "Cannot process empty text.", res_text.Ok)

	arg_text.Text = "Hello World!"
	res_text, err = textClient.ProcessText(context.Background(), arg_text)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_text.Ok)
	assert.Equal(t, 0, len(res_text.Usermentions))
	assert.Equal(t, 0, len(res_text.Urls))
	assert.Equal(t, "Hello World!", res_text.Text)

	arg_text.Text =
		"First post! @user_1@user_2 http://www.google.com/q=appleee @user_4 https://www.binggg.com Over!"
	res_text, err = textClient.ProcessText(context.Background(), arg_text)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_text.Ok)
	assert.Equal(t, 3, len(res_text.Usermentions))
	assert.Equal(t, int64(1), res_text.Usermentions[0])
	assert.Equal(t, int64(2), res_text.Usermentions[1])
	assert.Equal(t, int64(4), res_text.Usermentions[2])
	assert.Equal(t, 2, len(res_text.Urls))
	sUrl1 := res_text.Urls[0]
	sUrl2 := res_text.Urls[1]
	expectedText := fmt.Sprintf("First post! @user_1@user_2 %v @user_4 %v Over!", sUrl1, sUrl2)
	assert.Equal(t, expectedText, res_text.Text)

	// check urls
	urlTestPort := "9001"
	ufcmd, err := StartFowarding("url", urlTestPort, "8087")
	assert.Nil(t, err)
	urlconn, err := dialer.Dial("localhost:" + urlTestPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	urlClient := urlpb.NewUrlClient(urlconn)
	assert.NotNil(t, urlClient)

	arg_get := &urlpb.GetUrlsRequest{Shorturls: []string{sUrl1, sUrl2}}
	res_get, err := urlClient.GetUrls(context.Background(), arg_get)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_get.Ok)
	assert.Equal(t, 2, len(res_get.Extendedurls))
	assert.Equal(t, "http://www.google.com/q=appleee", res_get.Extendedurls[0])
	assert.Equal(t, "https://www.binggg.com", res_get.Extendedurls[1])

	// Stop fowarding
	assert.Nil(t, fcmd.Process.Kill())
	assert.Nil(t, ufcmd.Process.Kill())
}

func TestCompose(t *testing.T) {
	// start forwarding
	composeTestPort, tlTestPort, homeTestPort := "9000", "9001", "9002"
	cfcmd, err := StartFowarding("compose", composeTestPort, "8081")
	assert.Nil(t, err)
	tfcmd, err := StartFowarding("timeline", tlTestPort, "8089")
	assert.Nil(t, err)
	hfcmd, err := StartFowarding("home", homeTestPort, "8090")
	assert.Nil(t, err)

	composeConn, err := dialer.Dial("localhost:" + composeTestPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	composeClient := composepb.NewComposeClient(composeConn)
	assert.NotNil(t, composeClient)

	tlConn, err := dialer.Dial("localhost:" + tlTestPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	tlClient := tlpb.NewTimelineClient(tlConn)
	assert.NotNil(t, tlClient)

	homeConn, err := dialer.Dial("localhost:" + homeTestPort, nil)
	assert.Nil(t, err, fmt.Sprintf("dialer error: %v", err))
	homeClient := homepb.NewHomeClient(homeConn)
	assert.NotNil(t, homeClient)

	// compose empty post not allowed
	arg_compose := &composepb.ComposePostRequest{}
	res_compose, err := composeClient.ComposePost(context.Background(), arg_compose)
	assert.Nil(t, err)
	assert.Equal(t, "Cannot compose empty post!", res_compose.Ok)

	// compose 2 posts
	arg_compose.Posttype = postpb.POST_TYPE_POST
	arg_compose.Userid = int64(1)
	arg_compose.Text = "First post! @user_3 http://www.google.com/q=bear"
	res_compose, err = composeClient.ComposePost(context.Background(), arg_compose)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_compose.Ok)

	arg_compose.Posttype = postpb.POST_TYPE_REPOST
	arg_compose.Userid = int64(1)
	arg_compose.Text = "Second post! https://www.facebook.com/ @user_2"
	res_compose, err = composeClient.ComposePost(context.Background(), arg_compose)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_compose.Ok)

	// check timelines: user_1 has two items
	arg_tl := &tlpb.ReadTimelineRequest{Userid: int64(1), Start: int32(0), Stop: int32(2)}
	res_tl, err := tlClient.ReadTimeline(context.Background(), arg_tl)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(res_tl.Posts))
	assert.Equal(t, "OK", res_tl.Ok)
	post1 := res_tl.Posts[1]
	post2 := res_tl.Posts[0]
	assert.True(t, strings.HasPrefix(post1.Text, "First post! @user_3 "))
	assert.True(t, strings.HasPrefix(post2.Text, "Second post! "))

	// check hometimelines:
	// user_0 has two items (follower), user_0 and user_3 have one item (mentioned)
	arg_home := &tlpb.ReadTimelineRequest{Userid: int64(0), Start: int32(0), Stop: int32(2)}
	res_home, err := homeClient.ReadHomeTimeline(context.Background(), arg_home)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(res_home.Posts))
	assert.Equal(t, "OK", res_home.Ok)
	assert.True(t, IsPostEqual(post2, res_home.Posts[0]))
	assert.True(t, IsPostEqual(post1, res_home.Posts[1]))

	arg_home = &tlpb.ReadTimelineRequest{Userid: int64(2), Start: int32(0), Stop: int32(1)}
	res_home, err = homeClient.ReadHomeTimeline(context.Background(), arg_home)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_home.Ok)
	assert.True(t, IsPostEqual(post2, res_home.Posts[0]))

	arg_home = &tlpb.ReadTimelineRequest{Userid: int64(3), Start: int32(0), Stop: int32(1)}
	res_home, err = homeClient.ReadHomeTimeline(context.Background(), arg_home)
	assert.Nil(t, err)
	assert.Equal(t, "OK", res_home.Ok)
	assert.True(t, IsPostEqual(post1, res_home.Posts[0]))

	// Stop forwarding
	assert.Nil(t, cfcmd.Process.Kill())
	assert.Nil(t, tfcmd.Process.Kill())
	assert.Nil(t, hfcmd.Process.Kill())
}
