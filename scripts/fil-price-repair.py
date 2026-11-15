"""Fixed missing-main-price recovery; never directly calls Saby or modifies site price."""
import importlib.util,json,os,re,shutil,subprocess,tempfile
from pathlib import Path
spec=importlib.util.spec_from_file_location('fil_price_guards',Path(__file__).with_name('production-release-preflight.py'));g=importlib.util.module_from_spec(spec);spec.loader.exec_module(g)
VERIFIED_PRICING_SOURCE='40b923ecb9da7fea64fde7871c4b1ca898c3d2a3'

def main():
    operation=os.environ['FIL_REPAIR_OPERATION'].removeprefix('fil-price-')
    if operation not in ('inspect','prepare','approve') or os.environ.get('GITHUB_ACTIONS')!='true':raise g.PreflightError('invalid_operation')
    if operation!='inspect' and os.environ.get('GITHUB_RUN_ATTEMPT')!='1':raise g.PreflightError('no_blind_replay')
    source=Path(os.environ['RELEASE_SOURCE_DIR']).resolve();sha=os.environ['EXPECTED_COMMIT']
    if not re.fullmatch('[0-9a-f]{40}',sha):raise g.PreflightError('invalid_sha')
    if operation=='approve' and sha!=VERIFIED_PRICING_SOURCE:raise g.PreflightError('dual_price_release_not_verified')
    if subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()!=sha or subprocess.check_output(['git','status','--porcelain'],cwd=source,text=True).strip():raise g.PreflightError('source_not_clean_or_exact')
    print(json.dumps({'phase':'read_deployed_application'}),flush=True)
    reader=g.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN',''));app=reader.get('/apps/229223')['app'];env=g.production_settings(app,sha)
    if app.get('status')!='active' or env.get('CATALOG_PUBLICATION_MODE')!='live' or env.get('CATALOG_PUBLICATION_LIVE_ENABLED')!='true':raise g.PreflightError('runtime_not_live_ready')
    print(json.dumps({'phase':'deployed_runtime_verified'}),flush=True)
    wrapper=source/'backend/internal/admin/approved_fil_price_repair.go';command=source/'backend/cmd/approved-fil-price-repair'
    if wrapper.exists() or command.exists():raise g.PreflightError('temporary_source_exists')
    try:
        shutil.copyfile(Path(__file__).with_name('fil-price-repair')/'admin_wrapper.go.txt',wrapper)
        command.mkdir();shutil.copyfile(Path(__file__).with_name('fil-price-repair')/'main.go.txt',command/'main.go')
        with tempfile.TemporaryDirectory() as directory:
            binary=Path(directory)/'fil-price'
            build=subprocess.run(['go','build','-o',str(binary),'./cmd/approved-fil-price-repair'],cwd=source/'backend',stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180)
            if build.returncode:raise g.PreflightError('canonical_price_repair_build_failed')
            print(json.dumps({'phase':'canonical_command_built'}),flush=True)
            if g.production_settings(reader.get('/apps/229223')['app'],sha)!=env:raise g.PreflightError('environment_changed')
            private={'DatabaseURL':env['DATABASE_URL'],'CA':env.get('DATABASE_SSL_CA','').strip().strip('"\'').replace('\\n','\n'),'VerifyTLS':env.get('DATABASE_SSL_VERIFY','true').strip().lower() not in ('false','0')}
            args=[str(binary),'-operation',operation]
            if operation=='approve':
                identity=os.environ.get('FIL_REPAIR_DRAFT_ID','');fingerprint=os.environ.get('FIL_REPAIR_FINGERPRINT','')
                if not identity.isdigit() or int(identity)<=0 or not fingerprint:raise g.PreflightError('exact_draft_confirmation_missing')
                args+=['-draft-id',identity,'-fingerprint',fingerprint]
            child={k:v for k,v in os.environ.items() if k not in ('TIMEWEB_CLOUD_TOKEN','GITHUB_TOKEN')}
            print(json.dumps({'phase':'run_canonical_'+operation}),flush=True)
            reply=subprocess.run(args,input=json.dumps(private),env=child,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=90)
            for line in reply.stdout.splitlines():print(json.dumps(json.loads(line),ensure_ascii=False,sort_keys=True),flush=True)
            return reply.returncode
    finally:
        wrapper.unlink(missing_ok=True);shutil.rmtree(command,ignore_errors=True)

if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception as exc:
        print(json.dumps(g.safe_failure(exc,{'phase':'fixed_fil_price_repair'})));raise SystemExit(1)
