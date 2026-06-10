#!/usr/bin/env python3
"""Quick MindBank diagnostic - focused on API/DB disconnect"""
import subprocess, json, os

def run(cmd):
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=10)
    return r.stdout.strip(), r.stderr.strip(), r.returncode

print("=" * 60)
print("MINDBANK QUICK DIAGNOSTIC")
print("=" * 60)

# 1. Check if API is running
print("\n1. API Status:")
out, err, code = run("curl -s http://localhost:8095/api/v1/health")
if code == 0:
    try:
        data = json.loads(out)
        print(f"   Status: {data.get('status', 'unknown')}")
        print(f"   Postgres: {data.get('postgres', 'unknown')}")
    except:
        print(f"   Response: {out[:100]}")
else:
    print("   API NOT RESPONDING")

# 2. Check DB tables
print("\n2. Database Tables:")
out, err, code = run("docker exec mindbank-mindbank-postgres-1 psql -U mindbank -d mindbank -tAc \"SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename;\"")
if code == 0 and out:
    tables = [t for t in out.split('\n') if t.strip()]
    print(f"   Found {len(tables)} tables:")
    for t in tables[:10]:
        print(f"     - {t}")
else:
    print("   NO TABLES FOUND")

# 3. Check node counts
print("\n3. Node Counts:")
out, err, code = run("curl -s http://localhost:8095/api/v1/nodes?count=true")
api_count = 0
if code == 0:
    try:
        data = json.loads(out)
        api_count = data.get('count', 0)
        print(f"   API: {api_count}")
    except:
        print(f"   API error: {out}")

out, err, code = run("docker exec mindbank-mindbank-postgres-1 psql -U mindbank -d mindbank -tAc \"SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL;\"")
db_count = 0
if code == 0 and out:
    try:
        db_count = int(out)
        print(f"   DB:  {db_count}")
    except:
        print(f"   DB error: {out}")
else:
    print(f"   DB:  ERROR - {err}")

if api_count != db_count:
    print(f"   ⚠️  MISMATCH: API={api_count}, DB={db_count}")

# 4. Check migrations
print("\n4. Migrations:")
out, err, code = run("docker exec mindbank-mindbank-postgres-1 psql -U mindbank -d mindbank -tAc \"SELECT COUNT(*) FROM _migrations;\"")
if code == 0 and out:
    print(f"   Applied: {out}")
else:
    print(f"   _migrations table NOT FOUND")

# 5. Check app connections
print("\n5. Database Connections:")
out, err, code = run("""docker exec mindbank-mindbank-postgres-1 psql -U mindbank -d mindbank -tAc "SELECT count(*) FROM pg_stat_activity WHERE datname='mindbank' AND backend_type='client backend' AND application_name != 'psql';" """)
if code == 0:
    print(f"   App connections: {out}")
else:
    print(f"   Error: {err}")

# 6. Check for data files
print("\n6. Local Data Files:")
out, err, code = run("ls -la /home/rat/mindbank/*.db /home/rat/mindbank/*.sqlite* 2>/dev/null")
if out:
    print(f"   Found: {out}")
else:
    print("   No local DB files")

print("\n" + "=" * 60)
