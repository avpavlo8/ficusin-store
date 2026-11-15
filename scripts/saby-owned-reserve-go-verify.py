#!/usr/bin/env python3
"""One owned no-fiscal integration probe of the exact candidate Go adapter."""
import importlib.util,json,os,subprocess,urllib.request,urllib.parse
from pathlib import Path
spec=importlib.util.spec_from_file_location('reads',Path(__file__).with_name('saby-realization-readback.py'))
r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
CANDIDATE='46d95fb74c2e9c5c133b1dd700d40222f1bbed29'

def main():
    if os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('GITHUB_RUN_ATTEMPT')!='1':raise r.guards.PreflightError('first_attempt_only_do_not_rerun_creation')
    source=Path(os.environ['RELEASE_SOURCE_DIR']).resolve()
    if subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()!=CANDIDATE:raise r.guards.PreflightError('wrong_candidate_source')
    env=r.guards.production_settings(r.guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223')['app'],os.environ['EXPECTED_COMMIT'])
    token=r.request(r.AUTH,{k:env.get(n,'') for k,n in [('app_client_id','SABY_APP_CLIENT_ID'),('app_secret','SABY_APP_SECRET'),('secret_key','SABY_SECRET_KEY')]}).get('token')
    if not token:raise r.guards.PreflightError('auth_failed')
    print('::add-mask::'+token)
    def balance():
        query=urllib.parse.urlencode({'pointId':278,'priceListId':19,'withBalance':'true','noStopList':'true','pageSize':100,'page':0})
        req=urllib.request.Request('https://api.sbis.ru/retail/v2/nomenclature/list?'+query,headers={'X-SBISAccessToken':token,'Accept':'application/json'})
        with urllib.request.build_opener(r.guards.NoRedirect()).open(req,timeout=40) as response:data=json.loads(response.read(2<<20))
        matches=[n for n in data.get('nomenclatures',[]) if str(n.get('id'))=='3899']
        if len(matches)!=1:raise r.guards.PreflightError('dedicated_test_catalog_not_unique')
        return matches[0].get('balance')
    before=balance()
    if before!=1:raise r.guards.PreflightError('owned_test_stock_changed_before_probe')
    driver=source/'backend/cmd/cashier-reserve-verify-owned/main.go';driver.parent.mkdir(parents=True,exist_ok=False)
    driver.write_text(Path(__file__).with_name('saby-owned-reserve-go-driver.go.txt').read_text())
    output=Path.cwd()/'saby-owned-reserve-go-evidence.json'
    child=os.environ.copy();child.pop('TIMEWEB_CLOUD_TOKEN',None)
    child.update({key:env[key] for key in ['SABY_APP_CLIENT_ID','SABY_APP_SECRET','SABY_SECRET_KEY']})
    child.update(EVIDENCE_PATH=str(output),VERIFIED_SOURCE_COMMIT=CANDIDATE)
    result=subprocess.run(['go','run','./cmd/cashier-reserve-verify-owned'],cwd=source/'backend',env=child)
    report=json.loads(output.read_text()) if output.exists() else {}
    if result.returncode or report.get('verified') is not True:
        print(json.dumps({'verified':False,'noDatabaseUsed':True,'state':report.get('state'),'attention':report.get('attention'),'doNotRerunCreation':True},ensure_ascii=False));return 1
    rows=report.get('state',{}).get('rows',[]);ids=[row.get('id') for row in rows]
    if not ids or any(not isinstance(i,int) or i<=0 for i in ids):raise r.guards.PreflightError('known_owned_native_rows_missing')
    r.READS.add('Reserve.ListByDn')
    reserve=r.rpc('Reserve.ListByDn',{'DocNomIds':ids},token,7)
    after=balance()
    if reserve is None or r.rows(reserve) or before!=after:raise r.guards.PreflightError('test_reserve_cleanup_or_stock_not_verified')
    report.update(beforeBalance=before,afterBalance=after,finalReserveEmpty=True,independentNativeReadback=True)
    output.write_text(json.dumps(report,ensure_ascii=False))
    print(json.dumps({'verified':True,'sourceCommit':CANDIDATE,'documentID':report['documentID'],'revisions':report['revisions'],'finalReserveEmpty':True,'balanceBefore':before,'balanceAfter':after,'siteDatabaseUsed':False,'paymentRequested':False,'fiscalRequested':False,'postingRequested':False},ensure_ascii=False))
    return 0

if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(r.guards.safe_failure(exc,{'phase':'owned_go_native_reserve_verification','doNotRerunCreation':True})))
        raise SystemExit(1)
