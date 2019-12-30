package borowszkybee

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/astaxie/beego"
	"github.com/beego/i18n"
)

type ExtendedController struct {
	beego.Controller
	Lang string
}

type langType struct {
	Lang string
	Name string
}

type BaseHTTPResponseModel struct {
	Data          interface{}
	StatusCode    float64
	StatusMessage string
}

type FullJwt struct {
	Token   string
	Expires string
}

var langTypes = []*langType{}

func (c *ExtendedController)GetTranslatedText(messageKey string) string {
	return i18n.Tr(c.Lang, messageKey);
}

func LoadLanguages() {
	langs := strings.Split(beego.AppConfig.String("lang_types"), "|")
	names := strings.Split(beego.AppConfig.String("lang_names"), "|")
	langTypes := make([]*langType, 0, len(langs))
	for i, v := range langs {
		langTypes = append(langTypes, &langType{
			Lang: v,
			Name: names[i],
		})
	}

	for _, lang := range langs {
		fmt.Println("Loading language: " + lang)
		if i18n.IsExist(lang) == false {
			if err := i18n.SetMessage(lang, "conf/"+"locale_"+lang+".ini"); err != nil {
				fmt.Println("Fail to set message file: " + err.Error())
				continue
			}
		}
		fmt.Println("Language: " + lang + " alreay loaded")
	}
}

// setLangVer sets site language version.
func (c *ExtendedController) SetLanguange() bool {
	isNeedRedir := false
	hasCookie := false
	LoadLanguages()

	// 1. Check URL arguments.
	lang := c.Input().Get("lang")

	// 2. Get language information from cookies.
	if len(lang) == 0 {
		lang = c.Ctx.GetCookie("lang")
		hasCookie = true
	} else {
		isNeedRedir = true
	}

	// Check again in case someone modify on purpose.
	if !i18n.IsExist(lang) {
		lang = ""
		isNeedRedir = false
		hasCookie = false
	}

	// 3. Get language information from 'Accept-Language'.
	if len(lang) == 0 {
		al := c.Ctx.Request.Header.Get("Accept-Language")
		if len(al) > 4 {
			al = al[:5] // Only compare first 5 letters.
			if i18n.IsExist(al) {
				lang = al
			}
		}
	}

	// 4. Default language is English.
	if len(lang) == 0 {
		lang = "en-US"
		isNeedRedir = false
	}

	curLang := langType{
		Lang: lang,
	}

	// Save language information in cookies.
	if !hasCookie {
		c.Ctx.SetCookie("lang", curLang.Lang, 1<<31-1, "/")
	}

	var langLength = len(i18n.ListLangs())

	fmt.Println(langLength - 1)

	restLangs := make([]*langType, 0, langLength-1)
	for _, v := range langTypes {
		if lang != v.Lang {
			restLangs = append(restLangs, v)
		} else {
			curLang.Name = v.Name
		}
	}

	// Set language properties.
	c.Lang = lang
	c.Data["Lang"] = curLang.Lang
	c.Data["CurLang"] = curLang.Name
	c.Data["RestLangs"] = restLangs

	return isNeedRedir
}

func (c *ExtendedController) PerformHTTPGet(relativeURL string, nullResponseMessage string) map[string]interface{} {
	sess := c.GetSession(beego.AppConfig.String("SessionName"))
	if sess == nil {
		c.Redirect("/account/login", 302)
		return nil
	}
	fullJwt := ExtractFullTokenFromSession(sess)
	getDetailsResponse, err := MakeHTTPGet(beego.AppConfig.String(relativeURL), fullJwt.Token)
	processError := c.ProcessInvalidHTTPResponse(err, getDetailsResponse, nullResponseMessage)
	if processError {
		return nil
	}
	viewData := getDetailsResponse.Data.([]interface{})[0].(map[string]interface{})
	return viewData
}

func (c *ExtendedController) PerformHTTPGetInterface(relativeURL string, nullResponseMessage string) []interface{} {
	sess := c.GetSession(beego.AppConfig.String("SessionName"))
	if sess == nil {
		c.Redirect("/account/login", 302)
		return nil
	}
	fullJwt := ExtractFullTokenFromSession(sess)
	getDetailsResponse, err := MakeHTTPGet(beego.AppConfig.String(relativeURL), fullJwt.Token)
	processError := c.ProcessInvalidHTTPResponse(err, getDetailsResponse, nullResponseMessage)
	if processError {
		return nil
	}
	viewData := getDetailsResponse.Data.([]interface{})
	return viewData
}

func (c *ExtendedController) PerformHTTPGetInterfaceNoAuth(relativeURL string, nullResponseMessage string) []interface{} {
	getDetailsResponse, err := MakeHTTPGet(beego.AppConfig.String(relativeURL), "")
	processError := c.ProcessInvalidHTTPResponse(err, getDetailsResponse, nullResponseMessage)
	if processError {
		return nil
	}
	viewData := getDetailsResponse.Data.([]interface{})
	return viewData
}

func ExtractFullTokenFromSession(session interface{}) FullJwt {
	outValue := FullJwt{}
	outValue.Token = session.(FullJwt).Token
	outValue.Expires = session.(FullJwt).Expires
	return outValue
}

func (c *ExtendedController) MakeHTTPPost(dataToPost interface{}, relativeUrl string, authToken string) BaseHTTPResponseModel {
	if dataToPost == nil {
		return BaseHTTPResponseModel{}
	}

	dataToPostReader := convertInterfaceToReader(dataToPost)
	var beegoController = &c.Controller

	flash := beego.NewFlash()

	client := &http.Client{}
	req, err := http.NewRequest("POST", beego.AppConfig.String("externalApiBaseUrl")+relativeUrl, dataToPostReader)
	if err != nil {
		flash.Error(err.Error())
		flash.Store(beegoController)
		return BaseHTTPResponseModel{}
	}

	prepareHTTPHeader(req, authToken)

	resp, err := client.Do(req)
	if err != nil {
		flash.Error(err.Error())
		flash.Store(beegoController)
		return BaseHTTPResponseModel{}
	}

	defer resp.Body.Close()
	responseBodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		flash.Error(err.Error())
		flash.Store(beegoController)
		return BaseHTTPResponseModel{}
	}

	responseBodyString := string(responseBodyBytes)
	fianlResponse, err := HTTPResponseProcessor(responseBodyString)
	if err != nil {
		flash.Error(err.Error())
		flash.Store(beegoController)
		return BaseHTTPResponseModel{}
	}

	return fianlResponse
}

func MakeHTTPGet(relativeUrl string, authToken string) (BaseHTTPResponseModel, error) {

	client := &http.Client{}
	req, err := http.NewRequest("GET", beego.AppConfig.String("externalApiBaseUrl")+relativeUrl, nil)
	if err != nil {
		return BaseHTTPResponseModel{}, err
	}

	prepareHTTPHeader(req, authToken)

	resp, err := client.Do(req)
	if err != nil {
		return BaseHTTPResponseModel{}, err
	}
	if resp.StatusCode == 401 {
		return BaseHTTPResponseModel{StatusCode: 401}, nil
	}

	defer resp.Body.Close()
	responseBodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return BaseHTTPResponseModel{}, err
	}

	responseBodyString := string(responseBodyBytes)
	return HTTPResponseProcessor(responseBodyString)
}

func convertInterfaceToReader(input interface{}) io.Reader {
	dataToPostBytes, _ := json.Marshal(input)
	return bytes.NewReader(dataToPostBytes)
}

func prepareHTTPHeader(request *http.Request, authToken string) *http.Request {
	request.Header.Add("Authorization", authToken)
	return request
}

func HTTPResponseProcessor(httpResponseValue string) (BaseHTTPResponseModel, error) {
	if len(httpResponseValue) <= 0 {
		return BaseHTTPResponseModel{}, nil
	}
	var extractedResponse map[string]interface{}

	err := json.Unmarshal([]byte(httpResponseValue), &extractedResponse)
	if err != nil {
		return BaseHTTPResponseModel{}, err
	}
	fmt.Println(extractedResponse)

	dataToReturn := BaseHTTPResponseModel{
		Data:          extractedResponse["Data"],
		StatusCode:    (extractedResponse["StatusCode"]).(float64),
		StatusMessage: (extractedResponse["StatusMessage"]).(string)}

	return dataToReturn, nil

}

func (c *ExtendedController) ProcessInvalidHTTPResponse(err error, getDetailsResponse BaseHTTPResponseModel, nullResponseMessage string) bool {
	var beegoController = &c.Controller
	flash := beego.NewFlash()
	if err != nil {
		flash.Error(err.Error())
		flash.Store(beegoController)
		return true
	}
	if getDetailsResponse.StatusCode == 401 {
		flash.Warning(i18n.Tr(c.Lang, "AuthSessionExpired"))
		flash.Store(beegoController)
		c.Redirect("/account/logout", 302)
		return true
	}
	if getDetailsResponse.Data == nil {
		flash.Warning(i18n.Tr(c.Lang, nullResponseMessage))
		flash.Store(beegoController)
		return true
	}
	return false
}

func (c *ExtendedController) ExtractRequestBody() []byte {
	requestBodyBytes, err := ioutil.ReadAll(c.Ctx.Request.Body)
	if err != nil {
		c.Ctx.Output.SetStatus(500)
		return make([]byte, 0)
	}
	return requestBodyBytes
}
