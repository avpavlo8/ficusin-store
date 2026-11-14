#!/usr/bin/env python3
"""Execute the canonical service for exactly seven owner-approved receipts, ticket 31489326.

Dry-run by default. No deployment, migration, refund or payment creation. Secrets
are passed only through the child process's stdin, never command arguments/logs.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

def module(name,file):
    spec=importlib.util.spec_from_file_location(name,Path(__file__).with_name(file));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m
guards=module('seven_guards','production-release-preflight.py')
assets=module('seven_tool_env','production-static-assets.py')

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--expected-commit',required=True);parser.add_argument('--apply',action='store_true');args=parser.parse_args()
    phase='workflow_guard'
    try:
        if os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('APPROVED_SUPPORT_TICKET')!='31489326':raise guards.PreflightError('approved_workflow_only')
        source=Path(os.environ.get('RELEASE_SOURCE_DIR','')).resolve()
        if not source.is_dir() or subprocess.check_output(['git','-C',str(source),'rev-parse','HEAD'],text=True).strip()!=args.expected_commit or subprocess.check_output(['git','-C',str(source),'status','--porcelain'],text=True).strip():raise guards.PreflightError('source_revision_not_clean')
        env=guards.production_settings(guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223').get('app',{}),args.expected_commit)
        if env.get('YOOKASSA_SHOP_ID','').strip()!='1420523' or not env.get('YOOKASSA_SECRET_KEY','').strip():raise guards.PreflightError('wrong_payment_shop')
        if env.get('DATABASE_SSL_VERIFY','true').lower() in ('false','0'):raise guards.PreflightError('verified_database_tls_required')
        target=source/'backend/cmd/approved-seven-settlements'
        if target.exists():raise guards.PreflightError('temporary_command_already_exists')
        phase='canonical_service_build_and_guard_tests'
        try:
            shutil.copytree(Path(__file__).with_name('seven-settlements'),target)
            tool_env=assets.tool_env()
            test=subprocess.run(['go','test','./cmd/approved-seven-settlements','-count=1','-v'],cwd=source/'backend',env=tool_env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180)
            if test.returncode:raise guards.PreflightError('receipt_guard_tests_failed')
            print(json.dumps({'phase':'guard_tests','ok':True}),flush=True)
            with tempfile.TemporaryDirectory(prefix='approved-seven-receipts-') as directory:
                binary=Path(directory)/'settlements'
                build=subprocess.run(['go','build','-o',str(binary),'./cmd/approved-seven-settlements'],cwd=source/'backend',env=tool_env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180)
                if build.returncode:raise guards.PreflightError('canonical_service_build_failed')
                # Recheck the actual deployed app/settings immediately before execution.
                if guards.production_settings(guards.TimewebReader(os.environ.get('TIMEWEB_CLOUD_TOKEN','')).get('/apps/229223').get('app',{}),args.expected_commit)!=env:raise guards.PreflightError('production_settings_changed')
                private={'DatabaseURL':env['DATABASE_URL'],'CA':env.get('DATABASE_SSL_CA','').strip().strip('"\'').replace('\\n','\n'),'Shop':env['YOOKASSA_SHOP_ID'].strip(),'Secret':env['YOOKASSA_SECRET_KEY'].strip(),'Tax':int(env.get('YOOKASSA_TAX_SYSTEM') or '0'),'VAT':int(env.get('YOOKASSA_VAT_CODE') or '1')}
                child_env={**tool_env,'GITHUB_ACTIONS':'true','APPROVED_SUPPORT_TICKET':'31489326'}
                phase='apply_seven_settlements' if args.apply else 'read_only_seven_preparation'
                child=subprocess.run([str(binary),*(['--apply'] if args.apply else [])],input=json.dumps(private),env=child_env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=400)
                for line in child.stdout.splitlines():
                    report=json.loads(line)
                    if not isinstance(report,dict):raise guards.PreflightError('invalid_receipt_command_report')
                    print(json.dumps(report,sort_keys=True),flush=True)
                return child.returncode
        finally:
            if target.exists():shutil.rmtree(target)
    except Exception as error:
        print(json.dumps(guards.safe_failure(error,{'phase':phase})));return 1

if __name__=='__main__':raise SystemExit(main())
