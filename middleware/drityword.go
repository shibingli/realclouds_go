package middleware

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/garyburd/redigo/redis"
	"github.com/jinzhu/gorm"
	"github.com/labstack/echo"
)

const (
	//USER_DICT_PATH *
	USER_DICT_PATH = "/tmp/userdict.txt"
)

//DrityWord *
type DrityWord struct {
	UserDictPath string
	DrityWordMap *map[string]string
	Gorm         *gorm.DB
	Mutex        sync.RWMutex
}

//MwDrityWord Drity word middleware
func (d *DrityWord) MwDrityWord(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Set("drityword", d)
		return next(c)
	}
}

//NewDrityWord *
func NewDrityWord(db *gorm.DB, userDictPath ...string) (drityWord *DrityWord, err error) {
	userDict := USER_DICT_PATH

	if len(userDictPath) > 0 {
		userDict = strings.TrimSpace(userDictPath[0])
	}

	err = db.AutoMigrate(&DrityWordDB{}).Error
	if nil != err {
		return nil, err
	}

	drityWord = &DrityWord{
		UserDictPath: strings.TrimSpace(userDict),
		Gorm:         db,
	}

	_, drityWords := FindDrityWords(db)

	drityWordMap := make(map[string]string)

	for _, drityWord := range drityWords {
		drityWordMap[drityWord.MD5] = drityWord.Value
	}

	drityWord.DrityWordMap = &drityWordMap

	if err = drityWord.WriteDrityWord(); nil != err {
		return
	}

	return
}

//WriteDrityWord *
func (d *DrityWord) WriteDrityWord() error {
	f, err := os.OpenFile(d.UserDictPath, os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, v := range *d.DrityWordMap {
		if len(v) > 0 {
			_, err := f.WriteString(v + "\n")
			if nil != err {
				return err
			}
		}
	}
	return nil
}

//Subscription *
func (d *DrityWord) Subscription(rPool *redis.Pool) error {
	ctx, cancel := context.WithCancel(context.Background())

	err := ListenPubSubChannels(ctx, rPool,
		func() error {
			fmt.Printf("\nDrity word subscription start...\n\n")
			return nil
		},
		func(channel string, message []byte) error {
			fmt.Printf("channel: %s, message: %v\n", channel, string(message))

			if string(message) == "goodbye" {
				cancel()
			}
			return nil
		},
		func(pattern, channel string, message []byte) error {
			fmt.Printf("pattern: %s, channel: %s, message: %v\n", pattern, channel, string(message))

			if string(message) == "goodbye" {
				cancel()
			}
			return nil
		},
		DRITYWORD_UP_SUBSCRIPTION_KEY)

	if nil != err {
		return err
	}

	return nil
}
