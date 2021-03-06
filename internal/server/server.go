// Package server contains the code for the server engine.
package server

import (
	"encoding/json"
	"fmt"
	"go-osmand-tracker/internal/filesystem"
	"go-osmand-tracker/internal/types"
	"log"
	"net/http"
	"runtime"
	"strconv"

	"crypto/tls"
	"github.com/asdine/storm"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

var (
	lastDatabaseAddition [1]types.Entry
	serverIdentifier     string = fmt.Sprintf("%s on %s %s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
)

// Listen spins up a webserver and listens for incoming connections
func Listen(config *types.Config, db *storm.DB) {

	if config.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	ginEngine := gin.New()        // Creates a router without any middleware by default
	ginEngine.Use(gin.Recovery()) // Recovery middleware recovers from any panics and writes a 500 if there was one.
	if config.Debug {
		ginEngine.Use(gin.Logger())
	} // Only print access logs when debug mode is active.

	if filesystem.DoesDirExist("./web/dist") {
		ginEngine.Use(static.Serve("/", static.LocalFile("./web/dist", false)))
	} else {
		ginEngine.GET("/", func(c *gin.Context) {
			c.Header("Server", serverIdentifier)
			c.Header("Content-Type", "text/plain")
			c.Writer.WriteHeader(501)
			c.String(http.StatusNotImplemented, "Sorry, %s is not implemented.", c.Request.RequestURI)
		})
	}

	ginEngine.GET("/retrieve", func(c *gin.Context) {
		c.Header("Server", serverIdentifier)
		c.Header("Content-Type", "application/json")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Writer.WriteHeader(200)

		var cnt uint16 = 1
		c.Request.URL.Query().Get("count")
		if len(c.Request.URL.Query().Get("count")) >= 1 {
			parsedCountValue, err := strconv.ParseUint(c.Request.URL.Query().Get("count"), 10, 16)
			if err != nil {
				if config.Debug {
					log.Println("Error parsing query parameter 'count'", err)
				}
			} else {
				cnt = uint16(parsedCountValue)
			}
		}

		var (
			entries      []types.Entry
			responseData []byte
		)

		if cnt > 1 {
			if config.Debug {
				log.Println("Fetching last location entry from database")
			}
			db.All(&entries, storm.Limit(int(cnt)), storm.Reverse())
			res, err := json.Marshal(entries)
			if err != nil {
				log.Fatal("Processing entries from database failed", err)
			}
			responseData = res
		} else {
			if config.Debug {
				log.Println("Fetching last location entry from memory")
			}
			res, err := json.Marshal(lastDatabaseAddition)
			if err != nil {
				log.Fatal("Processing last entry from memory failed", err)
			}
			responseData = res
		}

		c.String(http.StatusOK, string(responseData))

	})

	ginEngine.GET("/submit", func(c *gin.Context) {
		c.Header("Server", serverIdentifier)
		c.Header("Content-Type", "application/json")
		c.Writer.WriteHeader(204) // The server successfully processed the request, and is not returning any content.

		// TODO: Error handling
		retrievedLatitude, _ := strconv.ParseFloat(c.Request.URL.Query().Get("lat"), 64)
		retrievedLongitude, _ := strconv.ParseFloat(c.Request.URL.Query().Get("lon"), 64)
		retrievedTimestamp, _ := strconv.ParseUint(c.Request.URL.Query().Get("timestamp"), 10, 64)
		retrievedHdop, _ := strconv.ParseFloat(c.Request.URL.Query().Get("hdop"), 64)
		retrievedAltitude, _ := strconv.ParseFloat(c.Request.URL.Query().Get("altitude"), 64)
		retrievedSpeed, _ := strconv.ParseFloat(c.Request.URL.Query().Get("speed"), 64)

		locationUpdate := types.LocationUpdate{
			Latitude:  retrievedLatitude,
			Longitude: retrievedLongitude,
			Timestamp: retrievedTimestamp,
			Hdop:      retrievedHdop,
			Altitude:  retrievedAltitude,
			Speed:     retrievedSpeed,
		}

		// Checks if the data (in 'locationUpdate') conforms to the types of the struct 'LocationUpdate'
		_, err := json.Marshal(locationUpdate)
		if err != nil {
			log.Fatal(err)
		}

		// Prepare and insert into DB
		entry := types.Entry{
			Timestamp: locationUpdate.Timestamp,
			Data:      locationUpdate,
		}
		err = db.Save(&entry)
		if err != nil {
			log.Fatal("Saving entry to database failed ", err)
		}

		lastDatabaseAddition[0] = entry
	})

	if config.Debug {
		log.Println("Fetching last location entry from database to place it in memory")
	}

	db.All(&lastDatabaseAddition, storm.Limit(int(1)), storm.Reverse())
	_, dbErr := json.Marshal(lastDatabaseAddition)
	if dbErr != nil {
		log.Fatal("Processing last entry from database failed", dbErr)
	}

	var listenAddr string = fmt.Sprintf(":%d", config.Port)
	if config.Debug {
		log.Printf("Starting server on port %v\n", config.Port)
	}

	var err error
	server := http.Server{
		Addr:    listenAddr,
		Handler: ginEngine,
	}

	if !config.NoTLS {
		tlsConfig := &tls.Config{
			MinVersion:       tls.VersionTLS12,
			CurvePreferences: []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		}

		server.TLSConfig = tlsConfig

		// validate if the crt & key is added
		if len(config.CRT) == 0 || len(config.CRT) == 0 {
			log.Fatal("invalid crt, Please add the TLS certificate and key!")
		}

		err = server.ListenAndServeTLS(config.CRT, config.KEY)
	} else {
		err = server.ListenAndServe()
	}

	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()
}
