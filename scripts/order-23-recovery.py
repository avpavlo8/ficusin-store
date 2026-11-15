"""Fixed order recovery using the deployed canonical service; read-only unless --apply."""
import argparse,importlib.util,json,os,shutil,subprocess,tempfile
from pathlib import Path
spec=importlib.util.spec_from_file_location('order23_guards',Path(__file__).with_name('production-release-preflight.py'));g=importlib.util.module_from_spec(spec);spec.loader.exec_module(g)

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--apply',action='store_true');args=parser.parse_args()
    if os.environ.get('GITHUB_ACTIONS')!='true' or args.apply and os.environ.get('GITHUB_RUN_ATTEMPT')!='1':raise g.PreflightError('approved_first_attempt_only')
    source=Path(os.environ['RELEASE_SOURCE_DIR']).resolve();sha=os.environ['EXPECTED_COMMIT']
    if subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()!=sha or subprocess.check_output(['git','status','--porcelain'],cwd=source,text=True).strip():raise g.PreflightError('source_revision_not_clean')
    reader=g.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN',''));app=reader.get('/apps/229223')['app'];env=g.production_settings(app,sha)
    if app.get('status')!='active' or env.get('ORDER_REALIZATION_MODE')!='live' or env.get('ORDER_REALIZATION_LIVE_ENABLED')!='true':raise g.PreflightError('live_runtime_not_ready')
    target=source/'backend/cmd/approved-order-23-recovery'
    if target.exists():raise g.PreflightError('temporary_command_exists')
    try:
        shutil.copytree(Path(__file__).with_name('order-23-recovery'),target)
        with tempfile.TemporaryDirectory() as directory:
            binary=Path(directory)/'order23'
            build=subprocess.run(['go','build','-o',str(binary),'./cmd/approved-order-23-recovery'],cwd=source/'backend',stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180)
            if build.returncode:raise g.PreflightError('fixed_recovery_build_failed')
            if g.production_settings(reader.get('/apps/229223')['app'],sha)!=env:raise g.PreflightError('settings_changed_before_recovery')
            private={'DatabaseURL':env['DATABASE_URL'],'CA':env.get('DATABASE_SSL_CA','').strip().strip('"\'').replace('\\n','\n'),'VerifyTLS':env.get('DATABASE_SSL_VERIFY','true').strip().lower() not in ('false','0')}
            childenv={k:v for k,v in os.environ.items() if k not in ('TIMEWEB_CLOUD_TOKEN','GITHUB_TOKEN')}
            result=subprocess.run([str(binary),*(['--apply'] if args.apply else [])],input=json.dumps(private),env=childenv,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=90)
            for line in result.stdout.splitlines():print(json.dumps(json.loads(line),sort_keys=True),flush=True)
            return result.returncode
    finally:shutil.rmtree(target)

if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(g.safe_failure(exc,{'phase':'fixed_order_23_recovery'})));raise SystemExit(1)
