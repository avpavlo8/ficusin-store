"""One approved metadata-only resume after independent native operator release.
Expected ops siblings: cashier-resume/main.go.txt, integration_wrapper.go.txt.
No native mutation is sent by this command; Execute is never called.
"""
import importlib.util,json,os,shutil,subprocess,tempfile
from pathlib import Path
spec=importlib.util.spec_from_file_location('resume_guards',Path(__file__).with_name('production-release-preflight.py'));g=importlib.util.module_from_spec(spec);spec.loader.exec_module(g)
VERIFIED_SOURCE='40b923ecb9da7fea64fde7871c4b1ca898c3d2a3'

def main():
    operation=os.environ['CASHIER_RESUME_OPERATION']
    if operation not in ('order-15-resume-inspect','order-15-resume-apply'):raise g.PreflightError('wrong_resume_operation')
    apply=operation=='order-15-resume-apply'
    if os.environ.get('GITHUB_ACTIONS')!='true' or apply and os.environ.get('GITHUB_RUN_ATTEMPT')!='1':raise g.PreflightError('approved_first_attempt_only')
    source=Path(os.environ['RELEASE_SOURCE_DIR']).resolve();sha=os.environ['EXPECTED_COMMIT']
    if sha!=VERIFIED_SOURCE:raise g.PreflightError('resume_release_not_verified')
    if subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()!=sha or subprocess.check_output(['git','status','--porcelain'],cwd=source,text=True).strip():raise g.PreflightError('wrong_or_dirty_source')
    print(json.dumps({'phase':'read_runtime'}),flush=True)
    reader=g.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN',''));app=reader.get('/apps/229223')['app'];env=g.production_settings(app,sha)
    required={'SABY_CASHIER_RESERVE_MODE':'live','SABY_CASHIER_RESERVE_LIVE_ENABLED':'true','SABY_CASHIER_RESERVE_WAREHOUSE_ID':'283','SABY_CASHIER_RESERVE_MOVEMENT_WAREHOUSE_ID':'284','SABY_CASHIER_RESERVE_ORGANIZATION_ID':'259','SABY_CASHIER_RESERVE_REGULATION_ID':'121326','SABY_CASHIER_RESERVE_BUYER_ID':'18749'}
    if app.get('status')!='active' or any(env.get(k)!=v for k,v in required.items()) or any(not env.get(k) for k in ('DATABASE_URL','SABY_APP_CLIENT_ID','SABY_APP_SECRET','SABY_SECRET_KEY')):raise g.PreflightError('verified_runtime_not_ready')
    target=source/'backend/cmd/approved-cashier-resume';wrapper=source/'backend/internal/integration/approved_cashier_resume.go'
    if target.exists() or wrapper.exists():raise g.PreflightError('temporary_command_exists')
    try:
        target.mkdir();files=Path(__file__).with_name('cashier-resume')
        shutil.copyfile(files/'main.go.txt',target/'main.go');shutil.copyfile(files/'integration_wrapper.go.txt',wrapper)
        with tempfile.TemporaryDirectory() as directory:
            binary=Path(directory)/'cashier-resume'
            build=subprocess.run(['go','build','-o',str(binary),'./cmd/approved-cashier-resume'],cwd=source/'backend',stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180)
            if build.returncode:raise g.PreflightError('resume_command_build_failed')
            if g.production_settings(reader.get('/apps/229223')['app'],sha)!=env:raise g.PreflightError('environment_changed')
            private={'DatabaseURL':env['DATABASE_URL'],'CA':env.get('DATABASE_SSL_CA','').strip().strip('"\'').replace('\\n','\n'),'VerifyTLS':env.get('DATABASE_SSL_VERIFY','true').strip().lower() not in ('false','0'),
              'SabyAppClientID':env['SABY_APP_CLIENT_ID'],'SabyAppSecret':env['SABY_APP_SECRET'],'SabySecretKey':env['SABY_SECRET_KEY'],
              'SabyPointID':int(env.get('SABY_POINT_ID') or '278'),'SabyPriceListID':int(env.get('SABY_PRICE_LIST_ID') or '6')}
            child={k:v for k,v in os.environ.items() if k not in ('TIMEWEB_CLOUD_TOKEN','GITHUB_TOKEN')}
            print(json.dumps({'phase':'metadata_resume' if apply else 'canonical_readonly_proof'}),flush=True)
            result=subprocess.run([str(binary),*(['--apply'] if apply else [])],input=json.dumps(private),env=child,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=110)
            for line in result.stdout.splitlines():print(json.dumps(json.loads(line),ensure_ascii=False,sort_keys=True),flush=True)
            return result.returncode
    finally:
        shutil.rmtree(target,ignore_errors=True);wrapper.unlink(missing_ok=True)

if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(g.safe_failure(exc,{'phase':'fixed_order15_reconciliation'})));raise SystemExit(1)
