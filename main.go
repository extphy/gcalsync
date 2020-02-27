package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"html/template"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"

	"github.com/extphy/gcalsync/env"
)

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config, genToken bool) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		if genToken {
			tok = getTokenFromWeb(config)
			saveToken(tokFile, tok)
		} else {
			log.Fatalf("cannot get token %v", err)
		}
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func doListCalendars(srv *calendar.Service) {

	calendars, err := srv.CalendarList.List().Do()
	if err != nil {
		log.Fatalf("Unable to retrieve calendar list", err)
	}
	if len(calendars.Items) > 0 {
		for _, calendar := range calendars.Items {
			fmt.Printf("\"%v\": %v\n", calendar.Summary, calendar.Id)
		}
	}
}

func syncSchedule(srv *calendar.Service, calendarId string, displayChunkPath string, printChunkPath string) {

	nw := time.Now()
//	   nw, err := time.Parse(time.RFC3339, "2019-04-07T15:04:05+03:00")
//	   if err != nil {
//	      log.Fatalf("time parse err: %v", err)
//	   }
//    log.Printf("now is: %v", nw)

	weekShift := -1
	if nw.Weekday() == time.Sunday {
		weekShift *= 6
	} else {
		weekShift *= int(nw.Weekday()) - 1
	}
	mon := nw.Add(time.Hour * 24 * time.Duration(weekShift))
	monMidnight := time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, mon.Location())
	sunMidnight := monMidnight.Add(time.Hour * 24 * 7)

	from := monMidnight.Format(time.RFC3339)
	to := sunMidnight.Format(time.RFC3339)

//   log.Printf("weekShift=%v monMidnight=%v sunMidnight=%v from=%v to=%v",
//         weekShift, monMidnight, sunMidnight, from, to)

	colors, err := srv.Colors.Get().Do()
	if err != nil {
		log.Fatalf("Unable to get colors: %v", err)
	} else {
		//		fmt.Printf("colors: %v", colors)
	}

	events, err := srv.Events.List(calendarId).ShowDeleted(false).
		SingleEvents(true).TimeMin(from).TimeMax(to).OrderBy("startTime").Do()
	if err != nil {
		log.Fatalf("Unable to query user events: %v", err)
	}
	if len(events.Items) == 0 {
		fmt.Println("No calendar events found.")
	} else {
		storeSchedule(events, colors, displayChunkPath, printChunkPath, monMidnight)
	}
}

func storeSchedule(events *calendar.Events, colors *calendar.Colors, displayChunkPath string, printChunkPath string, filterTime time.Time) {

	tmpl := template.Must(template.ParseFiles("tmpl/display.html", "tmpl/print.html"))

	displayFile, err := os.Create(fmt.Sprintf("%s.part", displayChunkPath))
	if err != nil {
		log.Fatalf("Unable to write display chunk: %v", err)
	}
	displayWriter := bufio.NewWriter(displayFile)

	printFile, err := os.Create(fmt.Sprintf("%s.part", printChunkPath))
	if err != nil {
		log.Fatalf("Unable to write print chunk: %v", err)
	}
	printWriter := bufio.NewWriter(printFile)

	ruWeek := []string{"Воскресенье", "Понедельник", "Вторник", "Среда", "Четверг", "Пятница", "Суббота", "Воскресенье"}
	ruWeekShort := []string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}
	weekClassSuffix := []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

	type EventData struct {
		TimeStart         string
      TimeEnd           string
      MinsStart         int
      MinsEnd           int
		Caption           string
      BackgroundColor   string
      ForegroundColor   string
	}

	type DayData struct {
		Caption      string
		ClassSuffix  string
		Events       []EventData
		DayNameShort string
		DayNum       string
	}

	var weekData []DayData
	var currentDay *DayData
	currentDay = nil
	prevWeekDay := -1

	for _, item := range events.Items {
		startDate, err := time.Parse(time.RFC3339, item.Start.DateTime)
		if err != nil {
			log.Fatalf("Unable to parse startDate %v: %v", item.Start.DateTime, err)
		}
      if startDate.Before(filterTime) {
         continue
      }

      endDate, err := time.Parse(time.RFC3339, item.End.DateTime)
      if err != nil {
         log.Fatalf("Unable to parse endDate %v: %v", item.End.DateTime, err)
      }
//		log.Printf("startDate=%s name=%s color=%s filtered=%v",
//            startDate, item.Summary, colors.Event[item.ColorId], startDate.Before(filterTime))

		if prevWeekDay != int(startDate.Weekday()) {
			weekData = append(weekData, DayData{
				fmt.Sprintf("%s %02d.%02d.%04d",
					ruWeek[int(startDate.Weekday())], startDate.Day(), startDate.Month(), startDate.Year()),
				weekClassSuffix[int(startDate.Weekday())],
				make([]EventData, 0, 0),
				ruWeekShort[int(startDate.Weekday())],
				fmt.Sprintf("%02d", startDate.Day()),
			})
			currentDay = &weekData[len(weekData)-1]
			prevWeekDay = int(startDate.Weekday())
		}
		currentDay.Events = append(currentDay.Events, EventData{
			fmt.Sprintf("%02d:%02d", startDate.Hour(), startDate.Minute()),
         fmt.Sprintf("%02d:%02d", endDate.Hour(), endDate.Minute()),
         startDate.Hour() * 60 + startDate.Minute(),
         endDate.Hour() * 60 + endDate.Minute(),
			template.HTMLEscapeString(strings.Trim(item.Summary, " ")),
         colors.Event[item.ColorId].Background,
         colors.Event[item.ColorId].Foreground,
		})
	}

	tmpl.ExecuteTemplate(printWriter, "print.html", weekData)
	printWriter.Flush()
	printFile.Close()
	os.Rename(fmt.Sprintf("%s.part", printChunkPath), printChunkPath)

	tmpl.ExecuteTemplate(displayWriter, "display.html", weekData)
	displayWriter.Flush()
	displayFile.Close()
	os.Rename(fmt.Sprintf("%s.part", displayChunkPath), displayChunkPath)
}

func main() {

	var configPath = flag.String("config", "./config.json", "configuration path")
	var tokenGen = flag.Bool("tkngen", false, "generate token")
	var listCal = flag.Bool("list", false, "list calendars")

	flag.Parse()

	b, err := ioutil.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, calendar.CalendarReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config, *tokenGen)

	srv, err := calendar.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve Calendar client: %v", err)
	}

	if *listCal {
		doListCalendars(srv)
	} else {
		config, err := env.LoadConfig(*configPath)
		if err != nil {
			log.Fatalf("Unable to load config %v", err)
		}
		syncSchedule(srv, config.CalendarId, config.DisplayOutput, config.PrintOutput)
	}
}
