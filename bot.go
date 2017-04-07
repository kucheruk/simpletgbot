package main

import (
	"fmt"
	"log"
	"time"

	"os"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/telegram-bot-api.v4"
)

type geoJSON struct {
	Type        string
	Coordinates [2]float64
}

type person struct {
	ID   int
	Lat  float64
	Lon  float64
	Loc  geoJSON
	When time.Time
}

type message struct {
	ID   bson.ObjectId
	Text string
	When time.Time
	Loc  geoJSON
}

func sendReply(bot *tgbotapi.BotAPI, id int64, text string) {
	reply := tgbotapi.NewMessage(id, text)
	reply.ReplyMarkup = kbd()
	bot.Send(reply)
}

func getLocation(db *mgo.Database, id int) (geoJSON, error) {
	result := person{}
	err := db.C("persons").Find(bson.M{"id": id}).One(&result)
	log.Printf("Query for _id=%d: %s", id, err)
	return result.Loc, err
}

func allMsgs(bot *tgbotapi.BotAPI, db *mgo.Database, upd tgbotapi.Update) {
	msg := upd.Message
	loc, err := getLocation(db, msg.From.ID)
	if err != nil {
		log.Printf("Error getting loc of user: %d %s", msg.From.ID, err.Error())
		sendReply(bot, msg.Chat.ID, "Set position first")
	} else {
		result := []message{}
		err2 := db.C("messages").Find(bson.M{
			"loc": bson.M{
				"$nearSphere": bson.M{
					"$geometry": bson.M{
						"type":        "Point",
						"coordinates": loc.Coordinates,
					},
					"$maxDistance": 200,
				},
			},
		}).Sort("-when").Limit(100).All(&result)
		log.Printf("RESULT QUERY %v %v", err2, result)
		if err2 != nil {
			sendReply(bot, msg.Chat.ID, err2.Error())
		} else {
			for _, m := range result {
				body := fmt.Sprintf("[%s] %s", m.When.Format("2006-01-02 15:04:05"), m.Text)
				sendReply(bot, msg.Chat.ID, body)
			}
		}
	}
}
func textHandler(bot *tgbotapi.BotAPI, db *mgo.Database, upd tgbotapi.Update) {
	msg := upd.Message
	loc, err := getLocation(db, msg.From.ID)
	if err != nil {
		log.Printf("Error getting loc of user: %d %s", msg.From.ID, err.Error())
		sendReply(bot, msg.Chat.ID, "Set position first")
	} else {
		envelope := message{
			ID:   bson.NewObjectIdWithTime(time.Now()),
			Text: msg.Text,
			When: time.Now(),
			Loc:  loc,
		}
		err2 := db.C("messages").Insert(&envelope)
		if err2 != nil {
			sendReply(bot, msg.Chat.ID, fmt.Sprintf("Error while saving message (%v)", err2))
		} else {
			sendReply(bot, msg.Chat.ID, "ok")
		}
	}
}

func meHandler(bot *tgbotapi.BotAPI, db *mgo.Database, upd tgbotapi.Update) {
	msg := upd.Message
	loc := msg.Location
	p := person{
		ID:   msg.From.ID,
		Loc:  geoJSON{Type: "Point", Coordinates: [2]float64{msg.Location.Latitude, msg.Location.Longitude}},
		When: time.Now(),
	}
	_, err := db.C("persons").Upsert(bson.M{"_id": p.ID}, &p)
	if err != nil {
		log.Printf("ERROR saving to mongo %v", err)
	}
	log.Printf("GOT location! %d = %f, %f",
		msg.From.ID,
		loc.Latitude,
		loc.Longitude)
	sendReply(bot, msg.Chat.ID, "Position set")
}

func tellHandler(upd tgbotapi.Update) {
	log.Printf("GOT %s", upd.Message.Text)
}

func kbd() tgbotapi.ReplyKeyboardMarkup {
	newMsgs := tgbotapi.NewKeyboardButton("/new")
	allMsgs := tgbotapi.NewKeyboardButton("/all")
	positionSet := tgbotapi.NewKeyboardButtonLocation("position")
	kbd := []tgbotapi.KeyboardButton{newMsgs, allMsgs, positionSet}
	return tgbotapi.NewReplyKeyboard(kbd)
}

func ensureIndices(db mgo.Database) {
	err := db.C("persons").EnsureIndex(mgo.Index{
		Key: []string{"_id"},
	})
	if err != nil {
		panic(err)
	}
	err = db.C("persons").EnsureIndex(mgo.Index{
		Key:  []string{"$2dsphere:loc"},
		Bits: 26,
	})
	if err != nil {
		panic(err)
	}
	err = db.C("messages").EnsureIndex(mgo.Index{
		Key:  []string{"$2dsphere:loc"},
		Bits: 26,
	})
	if err != nil {
		panic(err)
	}
	err = db.C("messages").EnsureIndex(mgo.Index{
		Key: []string{"when"},
	})
	if err != nil {
		panic(err)
	}
}

func openDb() *mgo.Database {
	session, err := mgo.Dial("localhost")
	// mgo.SetLogger(log.New(os.Stdout, "MONGO:", log.Ldate|log.Ltime|log.Lshortfile))
	// mgo.SetDebug(true)
	if err != nil {
		panic(err)
	}
	db := session.DB("geobot")
	ensureIndices(*db)
	return db
}

func main() {
	bot, err := tgbotapi.NewBotAPI(os.Args[1])
	if err != nil {
		log.Panic(err)
	}

	db := openDb()

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}
		if update.Message.Text == "/new" {
			tellHandler(update)
			continue
		}
		if update.Message.Text == "/all" {
			allMsgs(bot, db, update)
			continue
		}
		if update.Message.Location != nil {
			meHandler(bot, db, update)
			continue
		}
		if update.Message.Text != "" {
			textHandler(bot, db, update)
			continue
		}
		log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)
		//		log.Printf("[%f:%f]", update.Message.Location.Latitude, update.Message.Location.Longitude)
		sendReply(bot, update.Message.Chat.ID, "test")
	}
	db.Session.Close()
}
