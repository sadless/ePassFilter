// main
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/sloonz/go-iconv"
	"golang.org/x/crypto/ssh/terminal"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

const (
	EVENT_ID_START            = "go_event('"
	EVENT_ID_END              = "', "
	CONTENT_START             = "style=\"padding:7px 5px 7px 5px; line-height:1.6;\" bgcolor=\"#FFFFFF\" align=\"left\">"
	CONTENT_END               = "</td>"
	TYPE_DAILY                = 1
	TYPE_CONTENT              = 2
	TYPE_TIP                  = 3
	TYPE_GOODS                = 4
	TYPE_COMPANY              = 5
	CURRENT_EVENT_ID_POSITION = "F9F5C6"
	TITLE_START               = "<!--EAP_SUBJECT-->"
	TITLE_END                 = "<!--/EAP_SUBJECT-->"
	DAILY                     = "icon_daily.gif"
	TIP_START                 = "style=\"padding:5px 5px 5px 5px; line-height:1.6;\" bgcolor=\"#FFFFFF\" align=\"left\">"
	TIP_END                   = "</td>"
	GOODS_START               = "3px 5px 0px 5px; word-break: break-all\">"
	GOODS_END                 = "</td>"
)

type FilterEntry struct {
	typeValue int
	regex     string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("필터 파일명을 입력해주세요.")
		return
	}
	// 필터 파일 읽어와서 배열로 만듦
	filterFile, error := os.Open(os.Args[1])
	if error != nil {
		fmt.Println("필터 파일이 없습니다.")
		return
	}
	defer filterFile.Close()
	scanner := bufio.NewScanner(filterFile)
	typeOrder := true
	var typeValue int
	var filter []FilterEntry
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) > 0 && line[0] != '#' {
			if typeOrder {
				typeValue, error = strconv.Atoi(line)
				if error != nil {
					fmt.Println("[필터 파일 오류]타입으로 변환하지 못했습니다. 해당 줄 :", line)
					return
				}
				if typeValue != TYPE_DAILY {
					typeOrder = false
				} else {
					filter = append(filter, FilterEntry{typeValue, ""})
				}
			} else {
				filter = append(filter, FilterEntry{typeValue, line})
				typeOrder = true
			}
		}
	}
	if !typeOrder {
		fmt.Println("[필터 파일 오류]정규표현식이 입력되지 않았습니다.")
		return
	}

	// ID, Password 입력 받음(id, password)
	var id string

	fmt.Print("ID : ")
	fmt.Scan(&id)
	fmt.Print("Password : ")
	passwordByte, _ := terminal.ReadPassword(syscall.Stdin)
	password := string(passwordByte)
	fmt.Println("")

	// 로그인 시도
	response, _ := http.PostForm("http://www.e-pass.co.kr/member/login_proc.asp",
		url.Values{"tm_userid": {url.QueryEscape(id)}, "tm_pwd": {url.QueryEscape(password)}})
	defer response.Body.Close()
	body, _ := ioutil.ReadAll(response.Body)

	bodyString := string(body)
	if strings.Index(bodyString, "cation.href") < 0 {
		fmt.Println("로그인에 실패했습니다.")
		return
	}

	// 쿠키 저장
	cookies := response.Cookies()

	// 이벤트 전체 페이지에서 첫 번째 이벤트 ID 가져오기
	client := http.Client{}
	request := cookieAddedRequest("http://e-pass.co.kr/event/all.asp", cookies)
	response, _ = client.Do(request)
	body, _ = ioutil.ReadAll(response.Body)
	html, _ := iconv.Conv(string(body), "utf-8", "euc-kr")
	index := strings.Index(html, EVENT_ID_START)
	if index < 0 {
		fmt.Println("이벤트가 없습니다.")
		return
	}
	eventId := html[index+len(EVENT_ID_START):]
	eventId = eventId[:strings.Index(eventId, EVENT_ID_END)]
	MF := 0
	for {
		url := "http://e-pass.co.kr/event/all_info.asp?" +
			"InNo=" + eventId + "&" +
			"MF=" + strconv.Itoa(MF)
		request = cookieAddedRequest(url, cookies)
		response, _ = client.Do(request)
		body, _ = ioutil.ReadAll(response.Body)
		html, error = iconv.Conv(string(body), "utf-8", "euc-kr")
		// 가끔 인코딩이 안될 때가 있음
		if error != nil {
			fmt.Println("인코딩에 실패했습니다. URL :", url)
			MF++
			eventId = nextEventId(string(body))
			if len(eventId) == 0 {
				break
			}
			continue
		}
		title := html[strings.Index(html, TITLE_START)+len(TITLE_START):]
		title = title[:strings.Index(title, TITLE_END)]
		content := ""
		tip := ""
		tipFind := false
		filtered := false
		company := ""
		var goods []string
		for _, entry := range filter {
			switch entry.typeValue {
			// 매일 응모
			case TYPE_DAILY:
				if strings.Index(html, DAILY) >= 0 {
					deleteEvent(eventId, cookies)
					filtered = true
				}
			// 행사 내용
			case TYPE_CONTENT:
				if len(content) == 0 {
					content = html[strings.Index(html, CONTENT_START)+len(CONTENT_START):]
					content = strings.TrimSpace(content[:strings.Index(content, CONTENT_END)])
				}
				regex, error := regexp.Compile(entry.regex)
				if error != nil {
					fmt.Println("[필터 파일 오류]정규표현식을 해석하지 못했습니다. 해당 표현식 : " + entry.regex)
					return
				}
				if regex.MatchString(content) {
					deleteEvent(eventId, cookies)
					filtered = true
				}
			// 응모 요령
			case TYPE_TIP:
				if !tipFind {
					index = strings.Index(html, TIP_START)
					if index >= 0 {
						tip = html[strings.Index(html, TIP_START)+len(TIP_START):]
						tip = strings.TrimSpace(tip[:strings.Index(tip, TIP_END)])
					}
					tipFind = true
				}
				if len(tip) > 0 {
					regex, error := regexp.Compile(entry.regex)
					if error != nil {
						fmt.Println("[필터 파일 오류]정규표현식을 해석하지 못했습니다. 해당 표현식 : " + entry.regex)
						return
					}
					if regex.MatchString(tip) {
						deleteEvent(eventId, cookies)
						filtered = true
					}
				}
			// 경품
			case TYPE_GOODS:
				if len(goods) == 0 {
					for goodsString := html; ; {
						index = strings.Index(goodsString, GOODS_START)
						if index < 0 {
							break
						}
						goodsString = goodsString[index+len(GOODS_START):]
						oneGoods := strings.TrimSpace(goodsString[:strings.Index(goodsString, GOODS_END)])
						goods = append(goods, oneGoods)
					}
				}
				regex, error := regexp.Compile(entry.regex)
				if error != nil {
					fmt.Println("[필터 파일 오류]정규표현식을 해석하지 못했습니다. 해당 표현식 : " + entry.regex)
					return
				}
				matchCount := 0
				for _, oneGoods := range goods {
					if regex.MatchString(oneGoods) {
						matchCount++
					}
				}
				if matchCount == len(goods) {
					deleteEvent(eventId, cookies)
					filtered = true
				}
			// 주최사
			case TYPE_COMPANY:
				if len(company) == 0 {
					company = title[strings.Index(title, "[")+1 : strings.Index(title, "]")]
				}
				regex, error := regexp.Compile(entry.regex)
				if error != nil {
					fmt.Println("[필터 파일 오류]정규표현식을 해석하지 못했습니다. 해당 표현식 : " + entry.regex)
					return
				}
				if regex.MatchString(company) {
					deleteEvent(eventId, cookies)
					filtered = true
				}
			}
			if filtered {
				break
			}
		}
		fmt.Print(title + " => ")
		if filtered {
			fmt.Println("삭제")
		} else {
			fmt.Println("통과")
			MF++
		}
		eventId = nextEventId(html)
		if len(eventId) == 0 {
			break
		}
	}
}

func cookieAddedRequest(url string, cookies []*http.Cookie) *http.Request {
	request, _ := http.NewRequest("GET", url, nil)

	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	return request
}

func deleteEvent(eventId string, cookies []*http.Cookie) {
	client := http.Client{}
	params := url.Values{"ApplyType": {"1,0"}, "InNo": {eventId}}
	request, _ := http.NewRequest("POST", "http://e-pass.co.kr/user/event_apply_detail.asp",
		bytes.NewBufferString(params.Encode()))
	request.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	client.Do(request)
}

func nextEventId(html string) string {
	index := strings.Index(html, CURRENT_EVENT_ID_POSITION)
	if index < 0 {
		return ""
	}
	eventId := html[index:]
	eventId = eventId[strings.Index(eventId, EVENT_ID_START)+len(EVENT_ID_START):]
	eventId = eventId[strings.Index(eventId, EVENT_ID_START)+len(EVENT_ID_START):]
	index = strings.Index(eventId, EVENT_ID_START)
	if index < 0 {
		return ""
	}
	eventId = eventId[index+len(EVENT_ID_START):]
	eventId = eventId[:strings.Index(eventId, EVENT_ID_END)]

	return eventId
}
