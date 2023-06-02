package test

import (
	"testing"
	"fmt"
	"github.com/stretchr/testify/assert"
	"socialnetworkk8/dialer"
	"context"
	urlpb "socialnetworkk8/services/url/proto"
)

func TestUrl(t *testing.T) {
	// start server
	testPort := "9000"
	fcmd, err := StartFowarding("url", testPort, "8084")
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


