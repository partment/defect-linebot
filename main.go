package main

import (
    "database/sql"
    "errors"
    "fmt"
    "log"
    "net/http"
    "os"
    "reflect"
    "regexp"
    "strconv"
    "strings"

    _ "github.com/go-sql-driver/mysql"
    "github.com/gorilla/mux"
    "github.com/joho/godotenv"
    "github.com/line/line-bot-sdk-go/v7/linebot"
    _ "github.com/mattn/go-sqlite3"
    "github.com/robfig/cron/v3"
)

// Declare Global Line Bot
var bot *linebot.Client

// Declare Global Local Database Interface
var db *sql.DB

// Declare Global Remote Database Interface
var rdb *sql.DB

// Declare Global Roadmarks Name
var defectnames map[string]string

func main() {
    // Load ENVs
    err := godotenv.Load()
    if err != nil {
        log.Fatal("Error loading .env file.")
    } else if checkENV() {
        log.Fatal("Env error, check .env file.")
    } else {
        log.Println("Env loaded.")
    }

    // Initialize Line Bot
    bot, err = linebot.New(os.Getenv("ChannelSecret"), os.Getenv("ChannelAccessToken"))
    if err != nil {
        log.Fatal("Failed intializing line bot, check credentials.")
    } else {
        log.Println("Line bot initialized.")
    }

    // Initialize Cron
    go cronJob()

    // Initialize Database
    db = intialLocalDatabase()
    rdb = intialRemoteDatabase()

    // Initialize Callback And Local API Interface
    router := mux.NewRouter()
    router.HandleFunc("/callback", callbackHandler)
    router.HandleFunc("/trigger", triggerHandler).Queries("id", `{id}`, "defects", `{defects}`)
    server := &http.Server{
        Addr:    fmt.Sprintf(":%s", os.Getenv("CallbackPort")),
        Handler: router,
    }
    log.Println("Start serving http.")
    log.Fatal(server.ListenAndServe())

}

func callbackHandler(w http.ResponseWriter, r *http.Request) {

    events, err := bot.ParseRequest(r)

    if err != nil {
        if err == linebot.ErrInvalidSignature {
            w.WriteHeader(400)
        } else {
            w.WriteHeader(500)
        }
        return
    }

    for _, event := range events {
        if event.Type == linebot.EventTypeMessage {
            switch message := event.Message.(type) {
            case *linebot.TextMessage:

                commandParameters := strings.Split(message.Text, " ")
                var id string
                switch event.Source.Type {
                case "user":
                    id = event.Source.UserID
                case "group":
                    id = event.Source.GroupID
                case "room":
                    id = event.Source.RoomID
                default:
                    replyTextMessage(event, "不支援的對話類型")
                    return
                }

                switch commandParameters[0] {
                case "sub":
                    arguments, err := argumentSplitter(commandParameters)
                    if err != nil {
                        replyTextMessage(event, "指令結尾不可為空白")
                        return
                    }

                    result, err := addSubscriber(id, arguments)

                    var response string
                    switch result {
                    case 0:
                        response = "訂閱缺陷種類" + strings.Join(arguments, " ") + "成功\n\n" + replyAllSubscribe(id)
                    case 1:
                        response = "訂閱全部缺陷種類成功\n\n" + replyAllSubscribe(id)
                    case 2:
                        response = "已經訂閱所有種類，此命令將被忽略\n若要取消訂閱所有種類請輸入unsub\n\n" + replyAllSubscribe(id)
                    case 3:
                        response = "命令格式不正確"
                    }

                    replyTextMessage(event, response)
                    if len(arguments) == 0 {
                        arguments = []string{"all"}
                    }
                    if err == nil {
                        log.Println(fmt.Sprintf("User %s subscribing %s.", id, strings.Join(arguments, " ")))
                    }
                case "unsub":
                    arguments, err := argumentSplitter(commandParameters)
                    if err != nil {
                        replyTextMessage(event, "指令結尾不可為空白")
                        return
                    }

                    result, err := removeSubscriber(id, arguments)

                    var response string
                    switch result {
                    case 0:
                        response = "取消訂閱缺陷種類" + strings.Join(arguments, " ") + "成功\n\n" + replyAllSubscribe(id)
                    case 1:
                        response = "取消訂閱全部缺陷種類成功\n\n" + replyAllSubscribe(id)
                    case 2:
                        response = "移除所有訂閱成功\n\n" + replyAllSubscribe(id)
                    case 3:
                        response = "命令格式不正確"
                    }

                    replyTextMessage(event, response)
                    if contains(arguments, "all") {
                        arguments = []string{"all"}
                    }
                    if err == nil {
                        log.Println(fmt.Sprintf("User %s quit subscribing %s.", id, strings.Join(arguments, " ")))
                    }
                case "list":
                    replyTextMessage(event, replyAllSubscribe(id))
                    log.Println(fmt.Sprintf("User %s listed.", id))
                case "inspect":
                    arguments, err := argumentSplitter(commandParameters)
                    if err != nil {
                        replyTextMessage(event, "指令結尾不可為空白")
                        return
                    }

                    replyTextMessage(event, inspect(id, arguments))
                    if contains(arguments, "all") {
                        log.Println(fmt.Sprintf("User %s inspected all types of defect.", id))
                        break
                    }
                    if len(arguments) >= 1 {
                        log.Println(fmt.Sprintf("User %s inspected %s.", id, strings.Join(arguments, " ")))
                        break
                    } else {
                        log.Println(fmt.Sprintf("User %s inspected subscribed.", id))
                        break
                    }
                case "help":
                    replyTextMessage(event, _help)
                case "leave":
                    if first := string(id[0]); first == "C" {
                        log.Println("Group " + id + " wants bot to leave.")
                        bot.LeaveGroup(id).Do()
                    } else if first == "R" {
                        log.Println("Room " + id + " wants bot to leave.")
                        bot.LeaveRoom(id).Do()
                    } else {
                        replyTextMessage(event, "一對一聊天無法離開")
                    }
                case "getid":
                    replyTextMessage(event, id)
                case "version":
                    replyTextMessage(event, _version)
                default:
                    replyTextMessage(event, "未知的命令，輸入help查看指令幫助")
                }
            default:
                replyTextMessage(event, "未知的命令，輸入help查看指令幫助")
            }
        }
    }
}

func triggerHandler(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id := vars["id"]
    defects := vars["defects"]
    if !matchString(`^(U|R|C)(\w{32})$`, id) || matchString(`^(all|D\d{2})(,(all|D\d{2}))*$`, defects) {
        fmt.Fprintf(w, "Format unaccepted.")
        return
    }
    args := strings.Split(defects, ",")
    response := inspect(id, args)
    message := linebot.NewTextMessage("🔔手動觸發訊息🔔\n\n" + response)
    bot.PushMessage(id, message).Do()
    fmt.Fprintf(w, "Request success.")
}

func addSubscriber(id string, arguments []string) (int, error) {
    for _, argument := range arguments {
        if !matchString(`^(D\d{2})$`, argument) {
            return 3, errors.New("")
        }
    }

    tx, _ := db.Begin()

    stmt, err := tx.Prepare("select count(*) from subscriber where `id` = ? and `subscribe` = 'all'")
    checkError(err)

    var all int
    err = stmt.QueryRow(id).Scan(&all)
    checkError(err)

    if all == 1 {
        err := tx.Commit()
        checkError(err)
        return 2, nil
    }

    if len(arguments) >= 1 {
        stmt, _ := tx.Prepare("insert into subscriber (`id`, `subscribe`) values (?, ?)")
        for _, argument := range arguments {
            if argument == "all" {
                return 3, nil
            }
            stmt.Exec(id, argument)
        }
        err = tx.Commit()
        checkError(err)

        return 0, nil
    } else {
        stmt, _ := tx.Prepare("insert into subscriber (`id`, `subscribe`) values (?, 'all')")
        stmt.Exec(id)
        err := tx.Commit()
        checkError(err)

        return 1, nil
    }
}

func removeSubscriber(id string, arguments []string) (int, error) {
    for _, argument := range arguments {
        if !matchString(`^D\d{2}|all$`, argument) {
            return 3, errors.New("")
        }
    }

    tx, _ := db.Begin()
    if contains(arguments, "all") {
        stmt, _ := tx.Prepare("delete from subscriber where id = ?")
        stmt.Exec(id)
        err := tx.Commit()
        checkError(err)

        return 2, nil
    }
    if len(arguments) >= 1 {
        stmt, _ := tx.Prepare("delete from subscriber where id = ? and subscribe = ?")
        for _, argument := range arguments {
            stmt.Exec(id, argument)
        }
        err := tx.Commit()
        checkError(err)

        return 0, nil
    } else {
        stmt, _ := tx.Prepare("delete from subscriber where id = ? and subscribe = 'all'")
        stmt.Exec(id)
        err := tx.Commit()
        checkError(err)

        return 1, nil
    }
}

func replyAllSubscribe(id string) string {
    tx, _ := db.Begin()

    var all int
    err := tx.QueryRow("select count(*) from subscriber where `id` = ? and `subscribe` = 'all'", id).Scan(&all)
    checkError(err)
    if all == 1 {
        tx.Commit()
        return "您目前訂閱了：\n全部"
    }

    subscribing, err := tx.Query("select subscribe from subscriber where `id` = ?", id)
    checkError(err)
    response := "您目前訂閱了："
    rowNums := 0
    defer subscribing.Close()
    for subscribing.Next() {
        var subscribe string
        subscribing.Scan(&subscribe)
        response += "\n" + subscribe
        rowNums += 1
    }
    if rowNums == 0 {
        response = "您目前沒有任何訂閱"
    }

    tx.Commit()
    return response
}

func inspect(id string, arguments []string) string {
    for _, argument := range arguments {
        if !matchString(`^D\d{2}|all$`, argument) {
            return "命令格式不正確"
        }
    }

    response := "過去一小時內："
    defects := retriveDefectNum(id, arguments)
    if len(defects) == 0 {
        response += "\n沒有新增任何資料"
        return response
    }
    for _, defect := range defects {
        if defectnames[defect.markid] == "" {
            response += "\n" + defect.markid + "新增了" + strconv.Itoa(defect.num) + "筆資料"
        } else {
            response += "\n" + defectnames[defect.markid] + "(" + defect.markid + ")新增了" + strconv.Itoa(defect.num) + "筆資料"
        }
    }

    return response
}

func retriveDefectNum(id string, arguments []string) []Defect {
    tx, _ := db.Begin()
    rtx, _ := rdb.Begin()
    defer func() {
        tx.Commit()
        rtx.Commit()
    }()

    var stmt *sql.Stmt
    var rows *sql.Rows
    var err error

    if contains(arguments, "all") { // Retrive All Types
        stmt, _ = rtx.Prepare("select markid, count(markid) from recv where timestamp(markdate, marktime) between convert_tz(date_sub(now(), interval 1 hour), 'system', '+08:00') and convert_tz(now(), 'system', '+08:00') group by markid")
        rows, err = stmt.Query()
    } else if len(arguments) >= 1 { // Retrive Specific Types
        args := make([]interface{}, len(arguments))
        for i, argument := range arguments {
            args[i] = argument
        }
        stmt, _ = rtx.Prepare(`select markid, count(markid) from recv where timestamp(markdate, marktime) between convert_tz(date_sub(now(), interval 1 hour), 'system', '+08:00') and convert_tz(now(), 'system', '+08:00') and markid in (?` + strings.Repeat(",?", len(args)-1) + `) group by markid`)
        rows, err = stmt.Query(args...)
    } else { // Retrive Subscribed Types
        var all int
        tx.QueryRow("select count(*) from subscriber where `id` = ? and `subscribe` = 'all'", id).Scan(&all)
        if all == 1 {
            stmt, _ = rtx.Prepare("select markid, count(markid) from recv where timestamp(markdate, marktime) between convert_tz(date_sub(now(), interval 1 hour), 'system', '+08:00') and convert_tz(now(), 'system', '+08:00') group by markid")
            rows, err = stmt.Query()
        } else {
            // Get User's Subscribing List and Search
            subscribing, _ := tx.Query("select subscribe from subscriber where `id` = ?", id)
            subscribes := []string{}
            rowNums := 0
            defer subscribing.Close()
            for subscribing.Next() {
                var subscribe string
                subscribing.Scan(&subscribe)
                subscribes = append(subscribes, subscribe)
                rowNums += 1
            }
            if rowNums == 0 {
                return []Defect{}
            }
            args := make([]interface{}, len(subscribes))
            for i, subscribe := range subscribes {
                args[i] = subscribe
            }
            stmt, _ = rtx.Prepare(`select markid, count(markid) from recv where timestamp(markdate, marktime) between convert_tz(date_sub(now(), interval 1 hour), 'system', '+08:00') and convert_tz(now(), 'system', '+08:00') and markid in (?` + strings.Repeat(",?", len(args)-1) + `) group by markid`)
            rows, err = stmt.Query(args...)
        }
    }

    checkError(err)
    defer rows.Close()

    var defects []Defect
    for rows.Next() {
        var defect Defect
        err = rows.Scan(&defect.markid, &defect.num)
        checkError(err)
        defects = append(defects, defect)
    }

    return defects
}

func cronJob() {
    cronTabs := strings.Split(os.Getenv("Crontab"), ";")
    cronJob := cron.New()
    for _, cronTab := range cronTabs {
        cronJob.AddFunc(cronTab, routineJob)
    }
    cronJob.Start()
}

func routineJob() {
    log.Println("Start cron job.")
    tx, _ := db.Begin()
    defer func() {
        tx.Commit()
    }()

    var stmt *sql.Stmt
    var rows *sql.Rows
    var err error

    stmt, _ = tx.Prepare(`select id from subscriber group by id`)
    rows, err = stmt.Query()
    checkError(err)
    defer rows.Close()
    var idList []string
    for rows.Next() {
        var id string
        err = rows.Scan(&id)
        checkError(err)
        idList = append(idList, id)
    }

    for _, id := range idList {
        response := inspect(id, []string{})
        message := linebot.NewTextMessage("🔔排程訊息🔔\n\n" + response)
        bot.PushMessage(id, message).Do()
    }
}

func replyTextMessage(event *linebot.Event, response string) {
    var err error
    if _, err = bot.ReplyMessage(event.ReplyToken, linebot.NewTextMessage(response)).Do(); err != nil {
        log.Println(err)
    }
}

func argumentSplitter(parameters []string) ([]string, error) {
    var arguments []string
    if length := len(parameters); parameters[len(parameters)-1] == "" {
        return arguments, errors.New("")
    } else if length == 1 {
        arguments = []string{}
    } else if length >= 2 {
        arguments = parameters[1:]
    }
    return arguments, nil
}

func checkENV() bool {

    /*
       true : there's a type error
       false : there's no type error
    */

    envList := [...]struct {
        name                string
        _type               reflect.Kind
        regexp              string
        allowEmpty          bool
        multiValueSeperator string
    }{
        {"ChannelSecret", reflect.String, ``, false, ``},
        {"ChannelAccessToken", reflect.String, ``, false, ``},
        {"CallbackPort", reflect.Int, ``, false, ``},
        {"DatabaseHost", reflect.String, ``, false, ``},
        {"DatabaseUser", reflect.String, ``, false, ``},
        {"DatabasePassword", reflect.String, ``, false, ``},
        {"DatabaseName", reflect.String, ``, false, ``},
        {"Crontab", reflect.String, `^((((\d+,)+\d+|(\d+(\/|-|#)\d+)|\d+L?|\*(\/\d+)?|L(-\d+)?|\?|[A-Z]{3}(-[A-Z]{3})?) ?){5,7})$|(@(annually|yearly|monthly|weekly|daily|hourly|reboot))|(@every (\d+(ns|us|µs|ms|s|m|h))+)`, true, `;`},
    }

    for _, env := range envList {
        if env._type == reflect.Int {
            _, err := strconv.Atoi(os.Getenv(env.name)) // Check if CallbackPort can be parsed as Int
            if err != nil {
                return true
            }
        }
        if os.Getenv(env.name) == "" {
            log.Println(env.name + " is empty.")
            if !env.allowEmpty {
                return true
            }
        }
        if env._type == reflect.String && env.regexp != "" {
            if os.Getenv(env.name) == "" && env.allowEmpty {
                return false
            }
            if env.multiValueSeperator != "" {
                envSeperateds := strings.Split(os.Getenv(env.name), env.multiValueSeperator)
                for _, envSeperated := range envSeperateds {
                    if match, _ := regexp.MatchString(env.regexp, envSeperated); !match {
                        log.Fatal(env.name + " is not matching it's regexp.")
                        return true
                    }
                }
            } else {
                if match, _ := regexp.MatchString(env.regexp, os.Getenv(env.name)); !match {
                    log.Fatal(env.name + " is not matching it's regexp.")
                    return true
                }
            }
        }
    }

    return false

}

func intialLocalDatabase() *sql.DB {

    db, err := sql.Open("sqlite3", "./data.db")

    if err != nil {
        log.Fatal("Loading local database error : ", err)
        os.Exit(1)
    } else {
        log.Println("Local database established.")
    }

    sql_table := `
    CREATE TABLE IF NOT EXISTS "subscriber" (
        "id"	varchar(33) PRIMARY KEY,
        "subscribe"	varchar(3),
        CONSTRAINT "id_subscribe" UNIQUE("id","subscribe")
    );
    `
    _, err = db.Exec(sql_table)
    checkError(err)

    return db

}

func intialRemoteDatabase() *sql.DB {

    databaseAuth := map[string]string{
        "Host":     os.Getenv("DatabaseHost"),
        "User":     os.Getenv("DatabaseUser"),
        "Password": os.Getenv("DatabasePassword"),
        "Name":     os.Getenv("DatabaseName"),
    }

    db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", databaseAuth["User"], databaseAuth["Password"], databaseAuth["Host"], 3306, databaseAuth["Name"]))
    checkError(err)
    _, err = db.Exec("do 1")

    if err != nil {
        log.Fatal("Loading remote database error : ", err)
        os.Exit(1)
    } else {
        log.Println("Remote database established.")
    }

    stmt, err := db.Prepare("select * from roadmark")
    if err != nil {
        log.Fatal("Loading roadmarks failed : ", err)
        os.Exit(1)
    } else {
        log.Println("Loaded roadmarks")
    }
    rows, _ := stmt.Query()

    defer stmt.Close()

    defectnames = make(map[string]string)
    for rows.Next() {
        var roadmark Roadmark
        err = rows.Scan(&roadmark.markid, &roadmark.name)
        checkError(err)
        defectnames[roadmark.markid] = roadmark.name
    }

    return db

}

func checkError(err error) {
    if err != nil {
        log.Fatal(err)
    }
}

func contains(s []string, str string) bool {
    for _, v := range s {
        if v == str {
            return true
        }
    }

    return false
}

func matchString(pattern string, s string) bool {
    match, err := regexp.MatchString(pattern, s)
    checkError(err)
    return match
}
