"""Read exact acknowledged draft before approved local regulation amendment."""
import importlib.util,json,os,shutil,subprocess,tempfile
from pathlib import Path
spec=importlib.util.spec_from_file_location('amendment_guards',Path(__file__).with_name('production-release-preflight.py'));g=importlib.util.module_from_spec(spec);spec.loader.exec_module(g)
READ_SOURCE='358bddc2528438822f1d38595b750306100c1dea'
VERIFIED_APPLY_SOURCE='40b923ecb9da7fea64fde7871c4b1ca898c3d2a3'

def main():
    apply=os.environ['ORDER74_AMENDMENT_OPERATION']=='order-74-amendment-apply'
    if os.environ.get('GITHUB_ACTIONS')!='true' or apply and os.environ.get('GITHUB_RUN_ATTEMPT')!='1':raise g.PreflightError('approved_first_attempt_only')
    source=Path(os.environ['RELEASE_SOURCE_DIR']).resolve();sha=os.environ['EXPECTED_COMMIT'];expected_source=sha if apply else READ_SOURCE
    if apply and sha!=VERIFIED_APPLY_SOURCE:raise g.PreflightError('amendment_release_not_verified')
    if subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()!=expected_source or subprocess.check_output(['git','status','--porcelain'],cwd=source,text=True).strip():raise g.PreflightError('wrong_or_dirty_source')
    print(json.dumps({'phase':'read_runtime'}),flush=True)
    reader=g.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN',''));app=reader.get('/apps/229223')['app'];env=g.production_settings(app,sha)
    if app.get('status')!='active' or env.get('ORDER_REALIZATION_MODE')!='live' or env.get('ORDER_REALIZATION_LIVE_ENABLED')!='true' or env.get('ORDER_REALIZATION_REGULATION_ID')!='122498':raise g.PreflightError('verified_runtime_not_ready')
    target=source/'backend/cmd/approved-order74-regulation-amendment'
    if target.exists():raise g.PreflightError('temporary_command_exists')
    try:
        target.mkdir();shutil.copyfile(Path(__file__).with_name('order-74-amendment')/'main.go.txt',target/'main.go')
        with tempfile.TemporaryDirectory() as directory:
            binary=Path(directory)/'order74-amendment'
            build=subprocess.run(['go','build','-o',str(binary),'./cmd/approved-order74-regulation-amendment'],cwd=source/'backend',stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180)
            if build.returncode:raise g.PreflightError('amendment_command_build_failed')
            if g.production_settings(reader.get('/apps/229223')['app'],sha)!=env:raise g.PreflightError('environment_changed')
            private={'DatabaseURL':env['DATABASE_URL'],'CA':env.get('DATABASE_SSL_CA','').strip().strip('"\'').replace('\\n','\n'),'VerifyTLS':env.get('DATABASE_SSL_VERIFY','true').strip().lower() not in ('false','0'),
              'SabyAppClientID':env.get('SABY_APP_CLIENT_ID',''),'SabyAppSecret':env.get('SABY_APP_SECRET',''),'SabySecretKey':env.get('SABY_SECRET_KEY',''),
              'SabyPointID':int(env.get('SABY_POINT_ID') or '278'),'SabyPriceListID':int(env.get('SABY_PRICE_LIST_ID') or '6')}
            child={k:v for k,v in os.environ.items() if k not in ('TIMEWEB_CLOUD_TOKEN','GITHUB_TOKEN')}
            print(json.dumps({'phase':'canonical_apply' if apply else 'independent_readonly_verification'}),flush=True)
            result=subprocess.run([str(binary),*(['--apply'] if apply else [])],input=json.dumps(private),env=child,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=140)
            for line in result.stdout.splitlines():print(json.dumps(json.loads(line),ensure_ascii=False,sort_keys=True),flush=True)
            return result.returncode
    finally:shutil.rmtree(target,ignore_errors=True)

if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(g.safe_failure(exc,{'phase':'fixed_order74_amendment'})));raise SystemExit(1)
