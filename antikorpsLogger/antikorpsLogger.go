package antikorpsLogger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var fileLock sync.Mutex

type Header map[string][]string

type RequestDebugInfo struct {
	Level   string
	Time    int64
	Message string

	Method           string
	URL              *url.URL
	Proto            string
	ProtoMajor       int
	ProtoMinor       int
	Header           Header
	Body             string
	ContentLength    int64
	TransferEncoding []string
	Close            bool
	Host             string
	Form             url.Values
	PostForm         url.Values
	//MultipartForm *multipart.Form

	Trailer    Header
	RemoteAddr string
	RequestURI string

	// TLS *tls.ConnectionState

	//Response *Response
	//ctx context.Context
}

type JsonKeyValue struct {
	Level string
	Time  int64
	Key   string
	Value string
}

type JsonRequestUrl struct {
	Level   string
	Time    int64
	Key     string
	UrlInfo *url.URL
}

type JsonHeader struct {
	Level string
	Time  int64
	Key   string
	Value Header
}

type JsonHttpHeader struct {
	Level string
	Time  int64
	Key   string
	Value http.Header
}

func (m *MyJsonLogger) CompactHttpHeader(level, key string, header http.Header) string {
	var jsonHttpHeader JsonHttpHeader
	jsonHttpHeader.Level = level
	jsonHttpHeader.Key = key
	jsonHttpHeader.Time = time.Now().Unix()
	jsonHttpHeader.Value = header

	jsonContent, jsonContentError := json.Marshal(jsonHttpHeader)
	if jsonContentError != nil {
		log.Println("antikorps error" + jsonContentError.Error())
		return ""
	}

	return CompactJSON(jsonContent)
}

func (m *MyJsonLogger) CompactRequestUrl(level, key string, url *url.URL) string {
	var jsonRequestUrl JsonRequestUrl
	jsonRequestUrl.Level = level
	jsonRequestUrl.Key = key
	jsonRequestUrl.Time = time.Now().Unix()
	jsonRequestUrl.UrlInfo = url

	jsonContent, jsonContentError := json.Marshal(jsonRequestUrl)
	if jsonContentError != nil {
		log.Println("antikorps error" + jsonContentError.Error())
		return ""
	}

	return CompactJSON(jsonContent)
}

func (m *MyJsonLogger) CompactHeader(level, key string, header Header) string {
	var jsonHeader JsonHeader
	jsonHeader.Level = level
	jsonHeader.Key = key
	jsonHeader.Time = time.Now().Unix()
	jsonHeader.Value = header

	jsonContent, jsonContentError := json.Marshal(jsonHeader)
	if jsonContentError != nil {
		log.Println("antikorps error" + jsonContentError.Error())
		return ""
	}

	return CompactJSON(jsonContent)
}

func (m *MyJsonLogger) CompactJsonKeyValue(level, key, value string) string {
	var jsonKeyValue JsonKeyValue
	jsonKeyValue.Level = level
	jsonKeyValue.Time = time.Now().Unix()
	jsonKeyValue.Key = key
	jsonKeyValue.Value = value

	jsonContent, jsonContentError := json.Marshal(jsonKeyValue)
	if jsonContentError != nil {
		log.Println("antikorps error" + jsonContentError.Error())
		return ""
	}

	return CompactJSON(jsonContent)
}

func (m *MyJsonLogger) LogRequestJson(r *http.Request, level, message string) string {
	var rq RequestDebugInfo
	rq.Level = level
	rq.Time = time.Now().Unix()
	rq.Message = message

	rq.Method = r.Method
	rq.URL = r.URL
	rq.Proto = r.Proto
	rq.ProtoMajor = r.ProtoMajor
	rq.ProtoMinor = r.ProtoMinor
	rq.Header = Header(r.Header)

	responseBody, responseBodyError := io.ReadAll(r.Body)
	if responseBodyError != nil {
		log.Println("antikorps error: " + responseBodyError.Error())
		return ""
	}
	rq.Body = string(responseBody)
	rq.ContentLength = r.ContentLength
	rq.TransferEncoding = r.TransferEncoding
	rq.Close = r.Close
	rq.Host = r.Host
	rq.Form = r.Form
	rq.PostForm = r.PostForm
	rq.Trailer = Header(r.Trailer)
	rq.RemoteAddr = r.RemoteAddr
	rq.RequestURI = r.RequestURI

	jsonContent, jsonContentError := json.Marshal(rq)
	if jsonContentError != nil {
		log.Println("antikorps error: " + jsonContentError.Error())
		return ""
	}

	return CompactJSON(jsonContent)
}

type MyJsonLogger struct {
	JsonPath      string
	CriticalError string
}

type JsonBasicLogger struct {
	Level   string
	Time    int64
	Message string
}

func NewJsonLogger(path string) MyJsonLogger {
	return MyJsonLogger{
		JsonPath: path,
	}
}

func getFileNameByDay() string {
	now := time.Now().Unix()
	// año, mes, día: 20060102
	unixTime := time.Unix(now, 0).Format("20060102")
	filename := fmt.Sprintf("%v_log.jsonl", unixTime)
	return filename
}

func (m *MyJsonLogger) WriteToFile(content string) {
	fileName := getFileNameByDay()
	fileLock.Lock()
	filePath := filepath.Join(m.JsonPath, fileName)
	f, e := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0664)
	if e != nil {
		log.Println("antikorps error opening jsonl file" + e.Error())
		return
	}
	defer f.Close()
	_, writeError := io.WriteString(f, content+"\r\n")
	if writeError != nil {
		log.Println("antikorps error logging to json" + writeError.Error())
		return
	}
	fileLock.Unlock()
}

func (m *MyJsonLogger) BasicLogCompact(message, level string) string {
	var jsonBasicLogger JsonBasicLogger
	jsonBasicLogger.Level = level
	jsonBasicLogger.Time = time.Now().Unix()
	jsonBasicLogger.Message = message

	jsonContent, jsonContentError := json.Marshal(jsonBasicLogger)
	if jsonContentError != nil {
		log.Println("antikorps error logging to json" + jsonContentError.Error())
		return ""
	}

	return CompactJSON(jsonContent)

}

func (m *MyJsonLogger) ErrorLevel(message string) {
	jsonMessage := m.BasicLogCompact(message, "ERROR")
	m.WriteToFile(jsonMessage)
}

func (m *MyJsonLogger) DebugLevel(message string) {
	jsonMessage := m.BasicLogCompact(message, "DEBUG")
	m.WriteToFile(jsonMessage)
}

func CompactJSON(content []byte) string {
	compactJson := &bytes.Buffer{}

	compactError := json.Compact(compactJson, content)
	if compactError != nil {
		log.Println("antikorps error:" + compactError.Error())
		return ""
	}

	return compactJson.String()
}
