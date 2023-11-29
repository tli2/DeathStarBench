package test

import (
	"crypto/sha256"
	"strconv"
	"fmt"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"socialnetworkk8/services/cacheclnt"
	"socialnetworkk8/services/user"
	"socialnetworkk8/tune"
	"os/exec"
	"time"
	"context"
)

const (
	NUSER = 10
	MONGO_FWD_PORT = "9090"
)

var tu *TestUtil

func StartFowarding(service, testPort, targetPort string) (*exec.Cmd, error) {
	cmd := exec.Command("kubectl", "port-forward", "svc/"+service, testPort+":"+targetPort)
	if err := cmd.Start(); err != nil {
		return nil,  err
	}
	time.Sleep(500*time.Millisecond)
	return cmd, nil
}

type TestUtil struct {
	mclnt  *mongo.Client
	cachec *cacheclnt.CacheClnt
	fcmd   *exec.Cmd
}

func makeTestUtil() (*TestUtil, error) {
	tune.Init()
	log.Info().Msg("Start cache and DB connections")
	cachec := cacheclnt.MakeCacheClnt()
	fcmd, err := StartFowarding("mongodb-sn", MONGO_FWD_PORT, "27017")
	if err != nil {
		log.Error().Msgf("Cannot forward mongodb port: %v", err)
		return nil, err
	}

	mongoUrl := "mongodb://localhost:" + MONGO_FWD_PORT
	client, err := mongo.Connect(
		context.Background(), options.Client().ApplyURI(mongoUrl).SetMaxPoolSize(2048))
	if err != nil {
		log.Error().Msgf("Cannot dial to Mongo: %v", err)
		return nil, err
	}
	log.Info().Msg("New mongo session successfull...")
	return &TestUtil{client, cachec, fcmd}, nil
}

func (tu *TestUtil) clearDB() error {
	log.Info().Msg("Removing mongo DB contents ...")
	tu.mclnt.Database("socialnetwork").Collection("post").DeleteMany(context.TODO(), &bson.M{})
	tu.mclnt.Database("socialnetwork").Collection("graph-follower").DeleteMany(context.TODO(), &bson.M{})
	tu.mclnt.Database("socialnetwork").Collection("graph-followee").DeleteMany(context.TODO(), &bson.M{})
	tu.mclnt.Database("socialnetwork").Collection("timeline").DeleteMany(context.TODO(), &bson.M{})
	tu.mclnt.Database("socialnetwork").Collection("url").DeleteMany(context.TODO(), &bson.M{})
	tu.mclnt.Database("socialnetwork").Collection("media").DeleteMany(context.TODO(), &bson.M{})
	log.Info().Msg("Re-ensuring mongo DB indexes ...")
	tu.mclnt.Database("socialnetwork").Collection("user").Indexes().CreateOne(
		context.TODO(), mongo.IndexModel{Keys: bson.D{{"username", 1}}})
	tu.mclnt.Database("socialnetwork").Collection("post").Indexes().CreateOne(
		context.TODO(), mongo.IndexModel{Keys: bson.D{{"postid", 1}}})
	tu.mclnt.Database("socialnetwork").Collection("graph-follower").Indexes().CreateOne(
		context.TODO(), mongo.IndexModel{Keys: bson.D{{"userid", 1}}})
	tu.mclnt.Database("socialnetwork").Collection("graph-followee").Indexes().CreateOne(
		context.TODO(), mongo.IndexModel{Keys: bson.D{{"userid", 1}}})
	tu.mclnt.Database("socialnetwork").Collection("url").Indexes().CreateOne(
		context.TODO(), mongo.IndexModel{Keys: bson.D{{"shorturl", 1}}})
	tu.mclnt.Database("socialnetwork").Collection("timeline").Indexes().CreateOne(
		context.TODO(), mongo.IndexModel{Keys: bson.D{{"userid", 1}}})
	tu.mclnt.Database("socialnetwork").Collection("media").Indexes().CreateOne(
		context.TODO(), mongo.IndexModel{Keys: bson.D{{"mediaid", 1}}})
	return nil
}

func (tu *TestUtil) initUsers() error {
	// create NUSER test users
	for i := 0; i < NUSER; i++ {
		suffix := strconv.Itoa(i)
		newUser := user.User{
			Userid: int64(i),
			Username: "user_" + suffix,
			Lastname: "Lastname" + suffix,
			Firstname: "Firstname" + suffix,
			Password: fmt.Sprintf("%x", sha256.Sum256([]byte("p_user_" + suffix)))}
		_, err := tu.mclnt.Database("socialnetwork").Collection("user").InsertOne(
			context.TODO(), &newUser)
		if err != nil {
			log.Fatal().Msg(err.Error())
			return err
		}
	}
	return nil
}

func (tu *TestUtil) initGraphs() error {
	//user i follows user i+1
	for i := 0; i < NUSER-1; i++ {
		_, err1 := tu.mclnt.Database("socialnetwork").Collection("graph-follower").UpdateOne(
			context.TODO(), &bson.M{"userid": int64(i+1)},
			&bson.M{"$addToSet": bson.M{"edges": int64(i)}}, options.Update().SetUpsert(true))
		_, err2 := tu.mclnt.Database("socialnetwork").Collection("graph-followee").UpdateOne(
			context.TODO(), &bson.M{"userid": int64(i)},
			&bson.M{"$addToSet": bson.M{"edges": int64(i+1)}}, options.Update().SetUpsert(true))
		if err1 != nil || err2 != nil {
			err := fmt.Errorf("error updating graph %v %v", err1, err2)
			log.Fatal().Msg(err.Error())
			return err
		}
	}
	return nil
}

func (tu *TestUtil) Close() {
	tu.mclnt.Disconnect(context.TODO())
	tu.fcmd.Process.Kill()
}

func init() {
	tu, _ = makeTestUtil()
	defer tu.Close()
	tu.clearDB()
	tu.initUsers()
	tu.initGraphs()
}
