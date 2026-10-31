import json
import os
import urllib.request

token = os.environ['TIMEWEB_CLOUD_TOKEN']
def get(path):
    req = urllib.request.Request('https://api.timeweb.cloud' + path, headers={'Authorization':'Bearer '+token})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)

def summary(row):
    allowed = ('id','name','type','status','preset_id','location','region','price','price_day','price_month','cpu','ram','disk','framework','branch_name','is_auto_deploy','project_id','provider_id','repository_id','domains','url','default_domain','login','instance_id','description','is_enabled','language','health_check','build_cmd','run_cmd')
    return {key:row[key] for key in allowed if key in row}

for path in ['/api/v1/apps/229223','/api/v1/apps','/api/v1/presets/apps','/api/v1/databases','/api/v1/databases/4192781/instances','/api/v1/databases/4192781/privileges']:
    try:
        data = get(path)
        print(path, 'root keys', list(data))
        for key, value in data.items():
            if key == 'app':
                print('env names',list(value.get('envs',{})))
                print('app keys', list(value), 'summary', json.dumps(summary(value),ensure_ascii=False))
                for nested in ('repository','deploy_settings','preset','provider','branch'):
                    if isinstance(value.get(nested),dict):
                        print(nested, 'keys',list(value[nested]), 'summary',json.dumps(summary(value[nested]),ensure_ascii=False))
            elif isinstance(value,list):
                print(key,json.dumps([summary(row) if isinstance(row,dict) else row for row in value],ensure_ascii=False))
    except Exception as e:
        print(path,type(e).__name__,str(e))
