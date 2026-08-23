package catalogenrichment

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalog"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogai"
	"github.com/avpavlo8/ficusin-store/backend/internal/photos"
	"github.com/jackc/pgx/v5/pgxpool"
)

type generator interface {
	Generate(context.Context, catalogai.Input, string) (catalogai.Proposal, error)
	GenerateCover(context.Context, string) ([]byte, string, error)
	Configured() bool
}

type Status struct { Pending int `json:"pending"`; Processing int `json:"processing"`; Done int `json:"done"`; Failed int `json:"failed"` }

type Worker struct {
	pool *pgxpool.Pool
	repository *admin.PostgresRepository
	ai generator
	storage *photos.Storage
	logger *slog.Logger
	actor admin.Actor
	once sync.Once
}

func New(pool *pgxpool.Pool, repository *admin.PostgresRepository, ai generator, storage *photos.Storage, logger *slog.Logger) *Worker {
	return &Worker{pool:pool, repository:repository, ai:ai, storage:storage, logger:logger}
}

func (worker *Worker) Start(ctx context.Context) {
	worker.once.Do(func(){ go worker.run(ctx) })
}

func (worker *Worker) Status(ctx context.Context) (Status,error) {
	var status Status
	err:=worker.pool.QueryRow(ctx, `SELECT
		COUNT(*) FILTER(WHERE text_status='pending' OR image_status='pending')::int,
		COUNT(*) FILTER(WHERE text_status='processing' OR image_status='processing')::int,
		COUNT(*) FILTER(WHERE text_status='done' AND image_status IN ('done','skipped'))::int,
		COUNT(*) FILTER(WHERE text_status='failed' OR image_status='failed')::int
		FROM catalog_ai_enrichment_jobs`).Scan(&status.Pending,&status.Processing,&status.Done,&status.Failed)
	return status,err
}

func (worker *Worker) run(ctx context.Context) {
	if worker.ai==nil || !worker.ai.Configured() { worker.logger.Warn("catalog enrichment disabled: OpenAI is not configured"); return }
	if _,err:=worker.pool.Exec(ctx,`UPDATE catalog_ai_enrichment_jobs SET text_status=CASE WHEN text_status='processing' THEN 'pending' ELSE text_status END,image_status=CASE WHEN image_status='processing' THEN 'pending' ELSE image_status END,updated_at=CURRENT_TIMESTAMP WHERE text_status='processing' OR image_status='processing'`);err!=nil{worker.logger.Error("catalog enrichment recovery failed","error",err);return}
	if err:=worker.pool.QueryRow(ctx, `SELECT customer_id,role FROM admin_users WHERE is_active AND customer_id IS NOT NULL ORDER BY role='owner' DESC,id LIMIT 1`).Scan(&worker.actor.CustomerID,&worker.actor.Role);err!=nil{
		worker.logger.Error("catalog enrichment has no audit actor","error",err);return
	}
	for index:=0;index<3;index++ { go worker.textLoop(ctx) }
	if worker.storage!=nil && worker.storage.Configured(){ for index:=0;index<2;index++{go worker.imageLoop(ctx)} } else {
		_,_ = worker.pool.Exec(ctx, `UPDATE catalog_ai_enrichment_jobs SET image_status='skipped',image_error='Хранилище изображений не настроено',updated_at=CURRENT_TIMESTAMP WHERE image_status='pending'`)
		worker.logger.Warn("catalog enrichment covers skipped: image storage is not configured")
	}
}

func (worker *Worker) textLoop(ctx context.Context){
	for ctx.Err()==nil{
		id,ok:=worker.claim(ctx,"text");if !ok{return}
		if err:=worker.enrichText(ctx,id);err!=nil{worker.fail(ctx,id,"text",err)} else { _,_=worker.pool.Exec(ctx,`UPDATE catalog_ai_enrichment_jobs SET text_status='done',text_error='',updated_at=CURRENT_TIMESTAMP WHERE product_id=$1`,id) }
	}
}

func (worker *Worker) imageLoop(ctx context.Context){
	for ctx.Err()==nil{
		id,ok:=worker.claim(ctx,"image");if !ok{time.Sleep(10*time.Second);continue}
		if err:=worker.enrichImage(ctx,id);err!=nil{worker.fail(ctx,id,"image",err)} else { _,_=worker.pool.Exec(ctx,`UPDATE catalog_ai_enrichment_jobs SET image_status='done',image_error='',updated_at=CURRENT_TIMESTAMP WHERE product_id=$1`,id) }
	}
}

func(worker *Worker) claim(ctx context.Context,kind string)(int64,bool){
	status:=kind+"_status";attempts:=kind+"_attempts"
	ready:="";if kind=="image"{ready=" AND text_status='done'"}
	query:=fmt.Sprintf(`WITH next AS (SELECT product_id FROM catalog_ai_enrichment_jobs WHERE %s='pending' AND %s<3%s ORDER BY product_id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE catalog_ai_enrichment_jobs job SET %s='processing',%s=%s+1,updated_at=CURRENT_TIMESTAMP FROM next WHERE job.product_id=next.product_id RETURNING job.product_id`,status,attempts,ready,status,attempts,attempts)
	var id int64;if err:=worker.pool.QueryRow(ctx,query).Scan(&id);err!=nil{return 0,false};return id,true
}

func(worker *Worker) fail(ctx context.Context,id int64,kind string,failure error){
	message:=failure.Error();if len(message)>1200{message=message[:1200]}
	query:=fmt.Sprintf(`UPDATE catalog_ai_enrichment_jobs SET %s_status=CASE WHEN %s_attempts>=3 THEN 'failed' ELSE 'pending' END,%s_error=$2,updated_at=CURRENT_TIMESTAMP WHERE product_id=$1`,kind,kind,kind)
	_,_=worker.pool.Exec(ctx,query,id,message);worker.logger.Error("catalog enrichment failed","product_id",id,"kind",kind,"error",failure)
}

func(worker *Worker) enrichText(ctx context.Context,id int64)error{
	var input catalogai.Input;var categoryID int64
	if err:=worker.pool.QueryRow(ctx,`SELECT product.name,COALESCE(product.saby_id,''),COALESCE(category.name,product.catalog_section),product.description,COALESCE(product.category_id,0) FROM products product LEFT JOIN categories category ON category.id=product.category_id WHERE product.id=$1 AND product.status='draft'`,id).Scan(&input.Name,&input.SabyCode,&input.Category,&input.CurrentDescription,&categoryID);err!=nil{return err}
	if categoryID>0{attributes,err:=worker.repository.EffectiveCategoryAttributes(ctx,categoryID);if err!=nil{return err};for _,item:=range attributes{if !item.Active||item.Excluded||item.Audience!="customer"||item.Scope!="product"{continue};options:=[]string{};for _,option:=range item.Options{if option.Active{options=append(options,option.Code)}};input.Attributes=append(input.Attributes,catalogai.Attribute{Code:item.Code,Name:item.Name,Type:item.DataType,Unit:item.Unit,Options:options})}}
	proposal,err:=worker.ai.Generate(ctx,input,"full");if err!=nil{return err}
	update:=admin.ProductUpdate{Name:&proposal.Name,LatinName:&proposal.LatinName,ShortDescription:&proposal.ShortDescription,Description:&proposal.Description,Attributes:filterAttributes(proposal.Attributes,input.Attributes)}
	if isPlantCategory(input.Category){
		update.CareInstructions=&proposal.CareInstructions
		passport:=toPassport(proposal.Passport);update.Passport=&passport;update.ImportantWarnings=&proposal.Warnings
	}
	if _,err=worker.repository.UpdateProduct(ctx,worker.actor,id,update);err!=nil{
		// Keep valuable editorial content even if one generated attribute has the
		// wrong type. The manager can fill that characteristic during moderation.
		update.Attributes=nil
		if _,fallback:=worker.repository.UpdateProduct(ctx,worker.actor,id,update);fallback!=nil{return fmt.Errorf("save generated card: %v; fallback: %w",err,fallback)}
	}
	_,err=worker.pool.Exec(ctx,`UPDATE catalog_ai_enrichment_jobs SET cover_prompt=$2 WHERE product_id=$1`,id,strings.TrimSpace(proposal.CoverPrompt));return err
}

func(worker *Worker) enrichImage(ctx context.Context,id int64)error{
	var prompt,name string
	if err:=worker.pool.QueryRow(ctx,`SELECT job.cover_prompt,product.name FROM catalog_ai_enrichment_jobs job JOIN products product ON product.id=job.product_id WHERE job.product_id=$1 AND job.text_status='done'`,id).Scan(&prompt,&name);err!=nil{return err}
	if strings.TrimSpace(prompt)==""{prompt="Каталожная фотография товара «"+name+"»: один товар целиком, по центру, тёплый светло-бежевый фон, мягкая естественная тень, без текста, логотипов, ценников и посторонних предметов."}
	image,contentType,err:=worker.ai.GenerateCover(ctx,prompt);if err!=nil{return err};key:=fmt.Sprintf("products/%d/ai-cover-%d.webp",id,time.Now().UnixNano());if err=worker.storage.Put(ctx,key,image,contentType);err!=nil{return err};url:=worker.storage.PublicURL(key)
	item,err:=worker.repository.AddUploadedProductMedia(ctx,worker.actor,id,"ai://catalog-enrichment/"+fmt.Sprint(time.Now().UnixNano()),url,url);if err!=nil{return err};return worker.repository.SetPrimaryProductMedia(ctx,worker.actor,id,item.ID)
}

func filterAttributes(values map[string]any,allowed []catalogai.Attribute)map[string]any{result:=map[string]any{};for _,item:=range allowed{if value,ok:=values[item.Code];ok{result[item.Code]=value}};return result}
func isPlantCategory(value string)bool{value=strings.ToLower(value);return strings.Contains(value,"растен")||value=="plants"}
func toPassport(value catalogai.Passport)catalog.PlantPassport{faq:=make([]catalog.FAQItem,0,len(value.FAQ));for _,item:=range value.FAQ{faq=append(faq,catalog.FAQItem{Question:item.Question,Answer:item.Answer})};return catalog.PlantPassport{Origin:value.Origin,Lighting:value.Lighting,Watering:value.Watering,Humidity:value.Humidity,Temperature:value.Temperature,Soil:value.Soil,Fertilizer:value.Fertilizer,Repotting:value.Repotting,CareDifficulty:value.CareDifficulty,GrowthRate:value.GrowthRate,MatureSize:value.MatureSize,Toxicity:value.Toxicity,Problems:value.Problems,Pests:value.Pests,FAQ:faq}}
