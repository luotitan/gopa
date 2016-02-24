/**
 * User: Medcl
 * Date: 13-7-8
 * Time: 下午4:45
 */
package main

import (
	"flag"
	"fmt"
	log "github.com/cihub/seelog"
	. "github.com/medcl/gopa/core/config"
	httpServ "github.com/medcl/gopa/core/http"
	logging "github.com/medcl/gopa/core/logging"
	fsstore "github.com/medcl/gopa/core/store/fs"
	task "github.com/medcl/gopa/core/tasks"
	"github.com/medcl/gopa/core/util"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var seedUrl string
var logLevel string
var runtimeConfig *RuntimeConfig
var version string

func getSeqStr(start []byte, end []byte, mix bool) []byte {
	if (len(start)) == len(end) {
		for i := range start {
			fmt.Println(start[i])
		}
		//		if(start>64 && end < 123){

		//		}
	}

	return nil
}

func shutdown(offsets []*RoutingOffset, quitChannels []*chan bool, offsets2 []*RoutingOffset, quitChannels2 []*chan bool, quit chan bool) {
	log.Debug("start shutting down")
	for i := range quitChannels {
		*quitChannels[i] <- true
		log.Debug("send exit signal to quit channel-1,", i)
	}

	for i, item := range quitChannels2 {
		if item != nil {
			*item <- true
		}
		log.Debug("send exit signal to quit channel-2,", i)
	}

	log.Info("sent quit signal to go routings done")

	//	for i:=range offsets{
	//		//TODO
	//		log.Info("persist offset,",i,":",offsets[i].Offset,",",offsets[i].shard)
	//	}

	//	log.Info("persist kafka offsets done")

	quit <- true
	log.Debug("finished shutting down")
}

var startTime time.Time

func printStartInfo() {
	fmt.Println("  __ _  ___  _ __   __ _ ")
	fmt.Println(" / _` |/ _ \\| '_ \\ / _` |")
	fmt.Println("| (_| | (_) | |_) | (_| |")
	fmt.Println(" \\__, |\\___/| .__/ \\__,_|")
	fmt.Println(" |___/      |_|          ")
	fmt.Println(" ")

	fmt.Println("[gopa] " + version + " is on")
	fmt.Println(" ")
	startTime = time.Now()
}

func printShutdownInfo() {
	fmt.Println("                         |    |                ")
	fmt.Println("   _` |   _ \\   _ \\   _` |     _ \\  |  |   -_) ")
	fmt.Println(" \\__, | \\___/ \\___/ \\__,_|   _.__/ \\_, | \\___| ")
	fmt.Println(" ____/                             ___/        ")
	fmt.Println("[gopa] "+version+" is down, uptime:", time.Now().Sub(startTime))
	fmt.Println(" ")
}

func main() {

	version = "0.6_SNAPSHOT"

	printStartInfo()
	defer logging.Flush()

	flag.StringVar(&seedUrl, "seed", "http://example.com", "the seed url,where everything starts")
	flag.StringVar(&logLevel, "log", "info", "setting log level,options:trace,debug,info,warn,error")

	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())

	logging.SetInitLogging(logLevel)

	runtimeConfig = InitOrGetConfig()

	runtimeConfig.Version= version

	runtimeConfig.LogLevel = logLevel

	logging.SetLogging(runtimeConfig.LogLevel, runtimeConfig.LogPath)


	if seedUrl == "" || seedUrl == "http://example.com" {
		log.Error("no seed was given. type:\"gopa -h\" for help.")
		os.Exit(1)
	}

	//	runtimeConfig.Storage =& fsstore.FsStore{}
	runtimeConfig.Storage = &fsstore.FsStore{}

	runtimeConfig.Storage.Init()

	//	if(runtimeConfig.)

	//	atr:="AZaz"
	//	btr:=[]byte(atr)
	//	fmt.Println(btr)
	//
	//	id:= getSeqStr([]byte("AA"),[]byte("ZZ"),false)
	//	fmt.Println(id)

	//pprof serves
	if runtimeConfig.GoProfEnabled {
		go func() {
			log.Info(http.ListenAndServe("localhost:6060", nil))
			log.Info("pprof server is up,http://localhost:6060/debug/pprof")
		}()
	}

	//http serves
	if runtimeConfig.HttpEnabled {
		go func() {
			httpServ.Start(runtimeConfig)
		}()
	}

	//adding default http protocol
	if !strings.HasPrefix(seedUrl, "http") {
		seedUrl = "http://" + seedUrl
	}

	maxGoRoutine := runtimeConfig.MaxGoRoutine
	fetchQuitChannels := make([]*chan bool, maxGoRoutine)   //shutdownSignal signals for each go routing
	fetchTaskChannels := make([]*chan []byte, maxGoRoutine) //fetchTask channels
	fetchOffsets := make([]*RoutingOffset, maxGoRoutine)    //kafka fetchOffsets

	parseQuitChannels := make([]*chan bool, 2) //shutdownSignal signals for each go routing
	//	parseQuitChannels := make([]*chan bool, MaxGoRoutine) //shutdownSignal signals for each go routing
	parseOffsets := make([]*RoutingOffset, maxGoRoutine) //kafka fetchOffsets

	shutdownSignal := make(chan bool, 1)
	finalQuitSignal := make(chan bool, 1)

	//handle exit event
	exitEventChannel := make(chan os.Signal, 1)
	signal.Notify(exitEventChannel, syscall.SIGINT)
	signal.Notify(exitEventChannel, os.Interrupt)
	go func() {
		s := <-exitEventChannel
		log.Debug("got signal:", s)
		if s == os.Interrupt || s.(os.Signal) == syscall.SIGINT {
			log.Warn("got signal:os.Interrupt,saving data and exit")
			//			defer  os.Exit(0)

			runtimeConfig.Storage.PersistBloomFilter()

			//wait workers to exit
			log.Info("waiting workers exit")
			go shutdown(fetchOffsets, fetchQuitChannels, parseOffsets, parseQuitChannels, shutdownSignal)
			<-shutdownSignal
			log.Info("workers shutdown")
			finalQuitSignal <- true
		}
	}()

	//start fetcher
	for i := 0; i < maxGoRoutine; i++ {
		quitC := make(chan bool, 1)
		taskC := make(chan []byte)

		fetchQuitChannels[i] = &quitC
		fetchTaskChannels[i] = &taskC
		offset := new(RoutingOffset)
		//		offset.Offset = initOffset(runtimeConfig, "fetch", i)
		offset.Shard = i
		fetchOffsets[i] = offset

		go task.FetchGo(runtimeConfig, &taskC, &quitC, offset)
	}

	c2 := make(chan bool, 1)
	parseQuitChannels[0] = &c2
	offset2 := new(RoutingOffset)
	//	offset2.Offset = initOffset(runtimeConfig, "parse", 0)
	offset2.Shard = 0
	parseOffsets[0] = offset2
	pendingFetchUrls := make(chan []byte)

	//fetch rule:all urls -> persisted to sotre -> fetched from store -> pushed to pendingFetchUrls -> redistributed to sharded goroutines -> fetch -> save webpage to store -> done
	//parse rule:url saved to store -> local path persisted to store -> fetched to pendingParseFiles -> redistributed to sharded goroutines -> parse -> clean urls -> enqueue to url store ->done

	//sending feed to task queue
	go func() {
		//notice seed will not been persisted
		log.Debug("sending feed to fetch queue,", seedUrl)
		pendingFetchUrls <- []byte(seedUrl)
	}()

	//start local saved file parser
	if runtimeConfig.ParseUrlsFromSavedFileLog {
		go task.ParseGo(pendingFetchUrls, runtimeConfig, &c2, offset2)
	}

	//redistribute pendingFetchUrls to sharded workers
	go func() {
		for {
			url := <-pendingFetchUrls
			if !runtimeConfig.Storage.CheckWalkedUrl(url) {

				if runtimeConfig.Storage.CheckFetchedUrl(url) {
					log.Warn("dont hit walk bloomfilter but hit fetch bloomfilter,also ignore,", string(url))
					runtimeConfig.Storage.AddWalkedUrl(url)
					continue
				}

				randomShard := 0
				if maxGoRoutine > 1 {
					randomShard = rand.Intn(maxGoRoutine - 1)
				}
				log.Debug("publish:", string(url), ",shard:", randomShard)
				runtimeConfig.Storage.AddWalkedUrl(url)
				*fetchTaskChannels[randomShard] <- url
			} else {
				log.Trace("hit walk or fetch bloomfilter,just ignore,", string(url))
			}
		}
	}()

	//load predefined fetch jobs
	if runtimeConfig.LoadTemplatedFetchJob {
		go func() {

			if util.CheckFileExists(runtimeConfig.TaskConfig.TaskDataPath + "/urls/template.txt") {

				templates := util.ReadAllLines(runtimeConfig.TaskConfig.TaskDataPath + "/urls/template.txt")
				ids := util.ReadAllLines(runtimeConfig.TaskConfig.TaskDataPath + "/urls/id.txt")

				for _, id := range ids {
					for _, template := range templates {
						log.Trace("id:", id)
						log.Trace("template:", template)
						url := strings.Replace(template, "{id}", id, -1)
						log.Debug("new task from template:", url)
						pendingFetchUrls <- []byte(url)
					}
				}
				log.Info("templated download is done.")

			}

		}()
	}

	//fetch urls from saved pages
	if runtimeConfig.LoadPendingFetchJobs {
		c3 := make(chan bool, 1)
		parseQuitChannels[1] = &c3
		offset3 := new(RoutingOffset)
		//		offset3.Offset = initOffset(runtimeConfig, "fetch_from_saved", 0)
		offset3.Shard = 0
		parseOffsets[1] = offset3
		go task.LoadTaskFromLocalFile(pendingFetchUrls, runtimeConfig, &c3, offset3)
	}

	//parse fetch failed jobs,and will ignore the walk-filter
	//TODO

	if runtimeConfig.LoadRuledFetchJob {
		log.Debug("start ruled fetch")
		go func() {
			if runtimeConfig.RuledFetchConfig.UrlTemplate != "" {
				for i := runtimeConfig.RuledFetchConfig.From; i <= runtimeConfig.RuledFetchConfig.To; i += runtimeConfig.RuledFetchConfig.Step {
					url := strings.Replace(runtimeConfig.RuledFetchConfig.UrlTemplate, "{id}", strconv.FormatInt(int64(i), 10), -1)
					log.Debug("add ruled url:", url)
					pendingFetchUrls <- []byte(url)
				}
			} else {
				log.Error("ruled template is empty,ignore")
			}
		}()

	}

	<-finalQuitSignal
	printShutdownInfo()
}
