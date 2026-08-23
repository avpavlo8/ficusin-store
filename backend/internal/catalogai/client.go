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
type Passport struct { Origin string `json:"origin"`; Lighting string `json:"lighting"`; Watering string `json:"watering"`; Humidity string `json:"humidity"`; Temperature string `json:"temperature"`; Soil string `json:"soil"`; Fertilizer string `json:"fertilizer"`; Repotting string `json:"repotting"`; CareDifficulty string `json:"careDifficulty"`; GrowthRate string `json:"growthRate"`; MatureSize string `json:"matureSize"`; Toxicity string `json:"toxicity"`; Problems string `json:"problems"`; Pests string `json:"pests"`; FAQ []FAQ `json:"faq"` }
type Proposal struct { Name string `json:"name"`; LatinName string `json:"latinName"`; ShortDescription string `json:"shortDescription"`; Description string `json:"description"`; CareInstructions string `json:"careInstructions"`; Attributes map[string]any `json:"attributes"`; Passport Passport `json:"passport"`; Warnings []string `json:"warnings"`; CoverPrompt string `json:"coverPrompt"` }

type Client struct { key,model string; http *http.Client }
func New(key,model string)*Client{return &Client{key:strings.TrimSpace(key),model:strings.TrimSpace(model),http:&http.Client{Timeout:90*time.Second}}}
func(c *Client)Configured()bool{return c!=nil&&c.key!=""&&c.model!=""}

func(c *Client)Generate(ctx context.Context,input Input,mode string)(Proposal,error){
 if !c.Configured(){return Proposal{},ErrDisabled}; raw,_:=json.Marshal(input)
 task:=map[string]string{"description":"Исправь название, определи латинское название и напиши короткое и полное продающее описание.","attributes":"Заполни только разрешённые категорийные характеристики.","care":"Напиши подробную инструкцию по уходу, структурированный паспорт, FAQ и важные предупреждения."}[mode];if task==""{return Proposal{},fmt.Errorf("unknown AI mode %q",mode)}
 prompt:=`Ты редактор российского магазина комнатных растений «Фикусин». `+task+` Работай быстро по устойчивым ботаническим знаниям. Не выдумывай сорт по размеру или коду. Если точная идентификация сомнительна, сохрани исходное название и оставь спорные поля пустыми. Используй спокойный русский язык без медицинских и недоказанных обещаний. Для enum используй только перечисленные options.`+"\nДанные:\n"+string(raw)
 faqSchema:=map[string]any{"type":"array","items":map[string]any{"type":"object","additionalProperties":false,"properties":map[string]any{"question":map[string]any{"type":"string"},"answer":map[string]any{"type":"string"}},"required":[]string{"question","answer"}}}
 passportProperties:=map[string]any{};passportRequired:=[]string{};for _,key:=range []string{"origin","lighting","watering","humidity","temperature","soil","fertilizer","repotting","careDifficulty","growthRate","matureSize","toxicity","problems","pests"}{passportProperties[key]=map[string]any{"type":"string"};passportRequired=append(passportRequired,key)};passportProperties["faq"]=faqSchema;passportRequired=append(passportRequired,"faq")
 allProperties:=map[string]any{"name":map[string]any{"type":"string"},"latinName":map[string]any{"type":"string"},"shortDescription":map[string]any{"type":"string"},"description":map[string]any{"type":"string"},"careInstructions":map[string]any{"type":"string"},"attributes":map[string]any{"type":"object","additionalProperties":true},"passport":map[string]any{"type":"object","additionalProperties":false,"properties":passportProperties,"required":passportRequired},"warnings":map[string]any{"type":"array","items":map[string]any{"type":"string"}}}
 fields:=map[string][]string{"description":{"name","latinName","shortDescription","description"},"attributes":{"attributes"},"care":{"careInstructions","passport","warnings"}}[mode];properties:=map[string]any{};for _,field:=range fields{properties[field]=allProperties[field]};schema:=map[string]any{"type":"object","additionalProperties":false,"properties":properties,"required":fields}
 body:=map[string]any{"model":c.model,"input":prompt,"reasoning":map[string]any{"effort":"low"},"text":map[string]any{"format":map[string]any{"type":"json_schema","name":"ficusin_product_draft","strict":false,"schema":schema}}}
 encoded,_:=json.Marshal(body);req,err:=http.NewRequestWithContext(ctx,http.MethodPost,"https://api.openai.com/v1/responses",bytes.NewReader(encoded));if err!=nil{return Proposal{},err};req.Header.Set("Authorization","Bearer "+c.key);req.Header.Set("Content-Type","application/json")
 response,err:=c.http.Do(req);if err!=nil{return Proposal{},err};defer response.Body.Close();data,_:=io.ReadAll(io.LimitReader(response.Body,4<<20));if response.StatusCode/100!=2{return Proposal{},fmt.Errorf("OpenAI HTTP %d: %s",response.StatusCode,strings.TrimSpace(string(data)))}
 var envelope struct{Output []struct{Content []struct{Type string `json:"type"`;Text string `json:"text"`} `json:"content"`} `json:"output"`};if json.Unmarshal(data,&envelope)!=nil{return Proposal{},errors.New("OpenAI вернул повреждённый ответ")};for _,item:=range envelope.Output{for _,part:=range item.Content{if part.Type=="output_text"{var result Proposal;if err:=json.Unmarshal([]byte(part.Text),&result);err!=nil{return Proposal{},err};return result,nil}}};return Proposal{},errors.New("OpenAI не вернул карточку")
}

func(c *Client)GenerateCover(ctx context.Context,prompt string)([]byte,string,error){
	if !c.Configured(){return nil,"",ErrDisabled};body,_:=json.Marshal(map[string]any{"model":"gpt-image-2","prompt":prompt,"size":"1024x1024","quality":"medium","output_format":"webp"});req,err:=http.NewRequestWithContext(ctx,http.MethodPost,"https://api.openai.com/v1/images/generations",bytes.NewReader(body));if err!=nil{return nil,"",err};req.Header.Set("Authorization","Bearer "+c.key);req.Header.Set("Content-Type","application/json");response,err:=c.http.Do(req);if err!=nil{return nil,"",err};defer response.Body.Close();raw,_:=io.ReadAll(io.LimitReader(response.Body,20<<20));if response.StatusCode/100!=2{return nil,"",fmt.Errorf("OpenAI image HTTP %d: %s",response.StatusCode,strings.TrimSpace(string(raw)))};var envelope struct{Data []struct{B64 string `json:"b64_json"`} `json:"data"`};if err:=json.Unmarshal(raw,&envelope);err!=nil||len(envelope.Data)==0{return nil,"",errors.New("OpenAI не вернул изображение")};image,err:=base64.StdEncoding.DecodeString(envelope.Data[0].B64);return image,"image/webp",err
}
