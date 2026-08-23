package catalogai

import (
	"encoding/base64"
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "strings"
 "time"
)

var ErrDisabled=errors.New("AI-подготовка не настроена")

type Attribute struct { Code string `json:"code"`; Name string `json:"name"`; Type string `json:"type"`; Unit string `json:"unit"`; Options []string `json:"options"` }
type Input struct { Name string `json:"name"`; SabyCode string `json:"sabyCode"`; Category string `json:"category"`; CurrentDescription string `json:"currentDescription"`; Attributes []Attribute `json:"allowedAttributes"` }
type FAQ struct { Question string `json:"question"`; Answer string `json:"answer"` }
type Proposal struct { Name string `json:"name"`; LatinName string `json:"latinName"`; ShortDescription string `json:"shortDescription"`; Description string `json:"description"`; CareInstructions string `json:"careInstructions"`; Attributes map[string]any `json:"attributes"`; FAQ []FAQ `json:"faq"`; Warnings []string `json:"warnings"`; CoverPrompt string `json:"coverPrompt"`; Sources []string `json:"sources"` }

type Client struct { key,model string; http *http.Client }
func New(key,model string)*Client{return &Client{key:strings.TrimSpace(key),model:strings.TrimSpace(model),http:&http.Client{Timeout:90*time.Second}}}
func(c *Client)Configured()bool{return c!=nil&&c.key!=""&&c.model!=""}

func(c *Client)Generate(ctx context.Context,input Input)(Proposal,error){
 if !c.Configured(){return Proposal{},ErrDisabled}; raw,_:=json.Marshal(input)
 prompt:=`Ты редактор российского магазина комнатных растений «Фикусин». Подготовь проверяемый черновик карточки. Исследуй товар по авторитетным источникам. Не выдумывай сорт по размеру или коду. Заполняй только allowedAttributes и используй только перечисленные enum options. Если факт не подтверждён — не добавляй его. Текст уникальный, спокойный, без медицинских и недоказанных обещаний. careInstructions должна быть подробной практической инструкцией. coverPrompt — промпт для единой каталожной обложки: реалистичное растение целиком, тёплый молочно-бежевый фон, мягкий дневной свет, без текста, без изменения ботанических признаков. Верни прямые URL источников.`+"\nДанные:\n"+string(raw)
 schema:=map[string]any{"type":"object","additionalProperties":false,"properties":map[string]any{"name":map[string]any{"type":"string"},"latinName":map[string]any{"type":"string"},"shortDescription":map[string]any{"type":"string"},"description":map[string]any{"type":"string"},"careInstructions":map[string]any{"type":"string"},"attributes":map[string]any{"type":"object","additionalProperties":true},"faq":map[string]any{"type":"array","items":map[string]any{"type":"object","additionalProperties":false,"properties":map[string]any{"question":map[string]any{"type":"string"},"answer":map[string]any{"type":"string"}},"required":[]string{"question","answer"}}},"warnings":map[string]any{"type":"array","items":map[string]any{"type":"string"}},"coverPrompt":map[string]any{"type":"string"},"sources":map[string]any{"type":"array","items":map[string]any{"type":"string"}}},"required":[]string{"name","latinName","shortDescription","description","careInstructions","attributes","faq","warnings","coverPrompt","sources"}}
 body:=map[string]any{"model":c.model,"input":prompt,"tools":[]map[string]any{{"type":"web_search"}},"text":map[string]any{"format":map[string]any{"type":"json_schema","name":"ficusin_product_draft","strict":false,"schema":schema}}}
 encoded,_:=json.Marshal(body);req,err:=http.NewRequestWithContext(ctx,http.MethodPost,"https://api.openai.com/v1/responses",bytes.NewReader(encoded));if err!=nil{return Proposal{},err};req.Header.Set("Authorization","Bearer "+c.key);req.Header.Set("Content-Type","application/json")
 response,err:=c.http.Do(req);if err!=nil{return Proposal{},err};defer response.Body.Close();data,_:=io.ReadAll(io.LimitReader(response.Body,4<<20));if response.StatusCode/100!=2{return Proposal{},fmt.Errorf("OpenAI HTTP %d: %s",response.StatusCode,strings.TrimSpace(string(data)))}
 var envelope struct{Output []struct{Content []struct{Type string `json:"type"`;Text string `json:"text"`} `json:"content"`} `json:"output"`};if json.Unmarshal(data,&envelope)!=nil{return Proposal{},errors.New("OpenAI вернул повреждённый ответ")};for _,item:=range envelope.Output{for _,part:=range item.Content{if part.Type=="output_text"{var result Proposal;if err:=json.Unmarshal([]byte(part.Text),&result);err!=nil{return Proposal{},err};return result,nil}}};return Proposal{},errors.New("OpenAI не вернул карточку")
}

func(c *Client)GenerateCover(ctx context.Context,prompt string)([]byte,string,error){
	if !c.Configured(){return nil,"",ErrDisabled};body,_:=json.Marshal(map[string]any{"model":"gpt-image-2","prompt":prompt,"size":"1024x1024","quality":"medium","output_format":"webp"});req,err:=http.NewRequestWithContext(ctx,http.MethodPost,"https://api.openai.com/v1/images/generations",bytes.NewReader(body));if err!=nil{return nil,"",err};req.Header.Set("Authorization","Bearer "+c.key);req.Header.Set("Content-Type","application/json");response,err:=c.http.Do(req);if err!=nil{return nil,"",err};defer response.Body.Close();raw,_:=io.ReadAll(io.LimitReader(response.Body,20<<20));if response.StatusCode/100!=2{return nil,"",fmt.Errorf("OpenAI image HTTP %d: %s",response.StatusCode,strings.TrimSpace(string(raw)))};var envelope struct{Data []struct{B64 string `json:"b64_json"`} `json:"data"`};if err:=json.Unmarshal(raw,&envelope);err!=nil||len(envelope.Data)==0{return nil,"",errors.New("OpenAI не вернул изображение")};image,err:=base64.StdEncoding.DecodeString(envelope.Data[0].B64);return image,"image/webp",err
}
