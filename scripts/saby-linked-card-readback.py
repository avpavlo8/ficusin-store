"""Read one known existing product. Never writes catalogue data or logs tokens."""
import json
import os
import re
import urllib.error
import urllib.request

def request(url, body, headers=None):
    content_type = "application/json-rpc;charset=utf-8" if "method" in body else "application/json;charset=utf-8"
    req = urllib.request.Request(url, data=json.dumps(body).encode(), headers={"Content-Type": content_type, "Accept":"application/json", **(headers or {})}, method="POST")
    try:
        response = urllib.request.urlopen(req, timeout=45)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read()
        try:
            text = raw.decode(response.headers.get_content_charset() or "utf-8")
        except UnicodeDecodeError:
            text = raw.decode("cp1251")
        return json.loads(text)

def fields(record):
    schema = record.get("s", [])
    values = record.get("d", [])
    return {field.get("n"): values[i] for i, field in enumerate(schema) if i < len(values)}

token = request("https://online.sbis.ru/oauth/service/", {"app_client_id": os.environ["SABY_APP_CLIENT_ID"], "app_secret": os.environ["SABY_APP_SECRET"], "secret_key": os.environ["SABY_SECRET_KEY"]}).get("token")
if not token:
    raise SystemExit("No service token returned")
print("::add-mask::" + token)
result = request("https://online.sbis.ru/service/?srv=1", {"jsonrpc":"2.0", "protocol":6, "method":"Номенклатура.Прочитать", "params":{"ИдО":1101, "Расширение":None}, "id":1}, {"X-SBISAccessToken":token})
if result.get("error"):
    raise SystemExit("Read-only nomenclature RPC rejected: " + str(result["error"].get("code")))
record = result.get("result", {})
flat = fields(record)
if str(flat.get("@Номенклатура")) != "1101" or str(flat.get("НомНомер")) != "X269652448" or str(flat.get("Идентификатор")) != "0f56c2bd-2669-473f-9b94-875d521998a7":
    raise SystemExit("Identity mismatch; no metadata printed")
print("Identity verified for requested product")
print("Schema field names:", json.dumps(list(flat),ensure_ascii=False))
for name, value in flat.items():
    if re.search(r"categor|категор|folder|раздел|parent|иерарх|шаблон|template", name or "", re.I):
        if isinstance(value, (str, int, float, bool)) or value is None:
            print("Category/folder scalar:", json.dumps({"field":name,"value":value},ensure_ascii=False))
        elif isinstance(value, dict):
            print("Category/folder nested schema:", json.dumps({"field":name,"keys":list(fields(value)) if "s" in value else list(value)},ensure_ascii=False))
        elif isinstance(value, list):
            safe = value if len(value) <= 8 and all(v is None or isinstance(v,(str,int,float,bool)) for v in value) else {"items":len(value)}
            print("Category/folder identifier:", json.dumps({"field":name,"value":safe,"schema":next((s for s in record.get("s",[]) if s.get("n")==name),{})},ensure_ascii=False))

lookup = request("https://online.sbis.ru/service/?srv=1", {"jsonrpc":"2.0", "protocol":6, "method":"NomenclatureCategory.Read", "params":{"Id":1041,"AdditionalFields":["Name","Identifier"],"Option":None}, "id":2}, {"X-SBISAccessToken":token})
if lookup.get("error"):
    print("Category read rejected:",lookup["error"].get("code"),str(lookup["error"].get("message",""))[:600])
else:
    entity=fields(lookup.get("result",{}))
    print("Category response schema:",json.dumps(list(entity),ensure_ascii=False))
    if str(entity.get("Id")) == "1041":
        print("Verified category metadata:",json.dumps({"id":entity.get("Id"),"name":entity.get("Name"),"identifier":entity.get("Identifier")},ensure_ascii=False))
    else:
        print("Category identity not confirmed; no values printed")

lookup = request("https://online.sbis.ru/service/?srv=1", {"jsonrpc":"2.0", "protocol":6, "method":"Nomenclature.Read", "params":{"ИдО":3333,"ИмяМетода":None}, "id":3}, {"X-SBISAccessToken":token})
if lookup.get("error"):
    print("Folder read rejected:",lookup["error"].get("code"),str(lookup["error"].get("message",""))[:600])
else:
    entity=fields(lookup.get("result",{}))
    print("Folder response schema:",json.dumps(list(entity),ensure_ascii=False))
    if str(entity.get("@Номенклатура")) == "3333":
        print("Verified folder metadata:",json.dumps({"id":entity.get("@Номенклатура"),"name":entity.get("Наименование"),"type":entity.get("ТипНоменклатуры")},ensure_ascii=False))
    else:
        print("Folder identity not confirmed; no values printed")

def describe(value, depth=0):
    if depth > 3: return {"type":type(value).__name__}
    if isinstance(value,dict):
        if isinstance(value.get("s"),list) and isinstance(value.get("d"),list):
            return {"schema":value["s"],"fields":{key:describe(val,depth+1) for key,val in fields(value).items()}}
        return {"fields":{key:describe(val,depth+1) for key,val in list(value.items())[:40]}}
    if isinstance(value,list):
        return {"length":len(value),"firstShape":describe(value[0],depth+1) if value else None}
    return {"type":type(value).__name__}

native = request("https://online.sbis.ru/service/?srv=1", {"jsonrpc":"2.0", "protocol":6, "method":"Nomenclature.Read", "params":{"Id":1101,"AdditionalFields":["CodeList","Images","ImageData","PriceData","Category","CategoryName","CategoryObject","Number","Name","NameBase","Description","Identifier","IsDraft","VAT","MeasureUnit","Path"],"Option":{"_type":"record","s":[],"d":[]}}, "id":4}, {"X-SBISAccessToken":token})
if native.get("error"):
    print("Native read rejected:",native["error"].get("code"))
else:
    native_record=native.get("result",{})
    flat_native=fields(native_record)
    print("Native record schema:",json.dumps(native_record.get("s",[]),ensure_ascii=False))
    if str(flat_native.get("Id")) == "1101" and flat_native.get("Number") == "X269652448" and flat_native.get("Identifier") == "0f56c2bd-2669-473f-9b94-875d521998a7":
        print("Native identity verified")
        for name in ["CodeList","Images","ImageData","PriceData","Category","CategoryObject","Folder","VAT","MeasureUnit"]:
            print("Native field shape:",json.dumps({"field":name,"shape":describe(flat_native.get(name))},ensure_ascii=False))
        print("Native draft flag:",json.dumps(flat_native.get("IsDraft")))
    else:
        print("Native identity not confirmed; no native values printed")

category_list = request("https://online.sbis.ru/service/?srv=1", {"jsonrpc":"2.0","protocol":6,"method":"NomenclatureCategory.List","params":{"Фильтр":{"_type":"record","s":[],"d":[]},"Сортировка":None,"Навигация":{"_type":"record","s":[{"n":"Страница","t":"Число целое"},{"n":"РазмерСтраницы","t":"Число целое"},{"n":"ЕстьЕще","t":"Логическое"}],"d":[0,1,True]},"ДопПоля":["Name","IsFolderUI","TypeObject","StateSystem","ContainsAnyItem","HasCharacteristicAccounting","CharacteristicsAccounting","Deleted","IsCategory"]},"id":5}, {"X-SBISAccessToken":token})
if category_list.get("error"):
    print("Category list rejected:",category_list["error"].get("code"))
else:
    result=category_list.get("result",{})
    print("Category list schema:",json.dumps(result.get("s",[]),ensure_ascii=False))
    print("Category list count:",len(result.get("d",[])))

def recordset_rows(record):
    schema=record.get('s',[]) if isinstance(record,dict) else []
    rows=record.get('d',[]) if isinstance(record,dict) else []
    names=[column.get('n') for column in schema]
    return [{name:row[i] for i,name in enumerate(names) if i<len(row)} for row in rows if isinstance(row,list)]
if 'flat_native' in globals() and str(flat_native.get('Id'))=='1101' and flat_native.get('Number')=='X269652448' and flat_native.get('Identifier')=='0f56c2bd-2669-473f-9b94-875d521998a7':
    print('Native CodeList controlled rows:',json.dumps([{key:row.get(key) for key in ('Id','Code','Type','Order')} for row in recordset_rows(flat_native.get('CodeList'))],ensure_ascii=False))
    print('Native PriceData controlled rows:',json.dumps([{**{key:row.get(key) for key in ('PriceList','MeasureUnit','CanEdit','IsUpdated')},'PriceValueType':type(row.get('Price')).__name__} for row in recordset_rows(flat_native.get('PriceData'))],ensure_ascii=False))
    print('Native Description shape:',json.dumps(describe(flat_native.get('Description')),ensure_ascii=False))
    print('Native ImageData count:',len(recordset_rows(flat_native.get('ImageData'))))
    print('Native location identifiers:',json.dumps({key:flat_native.get(key) for key in ('Category','CategoryName','Folder')},ensure_ascii=False))
if 'category_list' in globals() and not category_list.get('error'):
    print('Category list controlled row:',json.dumps([{key:row.get(key) for key in ('Id','Name','IsCategory','IsFolder','IsFolderUI','Deleted')} for row in recordset_rows(category_list.get('result',{}))],ensure_ascii=False))

def markup_shape(value, depth=0):
    if depth>6:return {'type':type(value).__name__}
    if isinstance(value,list):return [markup_shape(v,depth+1) for v in value[:12]]
    if isinstance(value,dict):return {key:markup_shape(val,depth+1) for key,val in list(value.items())[:15]}
    if isinstance(value,str):return {'literal':value} if value in ('p','div','span','br','text','a','ul','li','b','i','strong','em','u','h1','h2','h3') else {'type':'text','length':len(value)}
    return {'type':type(value).__name__}
if 'flat_native' in globals() and str(flat_native.get('Id'))=='1101' and flat_native.get('Number')=='X269652448' and flat_native.get('Identifier')=='0f56c2bd-2669-473f-9b94-875d521998a7':
    print('Native Description markup structure:',json.dumps(markup_shape(flat_native.get('Description')),ensure_ascii=False))
    print('Native PriceData row identities:',json.dumps([{key:row.get(key) for key in ('Id','PriceList','MeasureUnit','CanEdit')} for row in recordset_rows(flat_native.get('PriceData'))],ensure_ascii=False))
price_scope = request('https://online.sbis.ru/service/?srv=1',{'jsonrpc':'2.0','protocol':6,'method':'Nomenclature.Read','params':{'Id':1101,'AdditionalFields':['PriceData','Number','Identifier'],'Option':{'_type':'record','s':[{'n':'PriceList','t':{'n':'Массив','t':'Число целое'}}],'d':[[6]]}},'id':6},{'X-SBISAccessToken':token})
if price_scope.get('error'):
    print('Scoped price read rejected:',price_scope['error'].get('code'))
else:
    scoped=fields(price_scope.get('result',{}))
    if str(scoped.get('Id'))=='1101' and scoped.get('Number')=='X269652448' and scoped.get('Identifier')=='0f56c2bd-2669-473f-9b94-875d521998a7':
        print('Scoped price identity verified')
        print('Scoped PriceData schema:',json.dumps(scoped.get('PriceData',{}).get('s',[]),ensure_ascii=False))
        print('Scoped PriceData controlled rows:',json.dumps([{**{key:row.get(key) for key in ('Id','PriceList','MeasureUnit','CanEdit','IsUpdated')},'PriceValueType':type(row.get('Price')).__name__} for row in recordset_rows(scoped.get('PriceData'))],ensure_ascii=False))
    else: print('Scoped price identity not confirmed; no values printed')
