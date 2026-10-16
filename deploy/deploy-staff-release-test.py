#!/usr/bin/env python3
"""Pure disposable helper tests; no installed paths, services or credentials."""
import importlib.machinery, json, pathlib, sqlite3, tempfile
module=importlib.machinery.SourceFileLoader('staff_deploy_test',str(pathlib.Path(__file__).with_name('deploy-staff-release.py'))).load_module()
with tempfile.TemporaryDirectory() as directory:
 root=pathlib.Path(directory);(root/'covers').mkdir();(root/'covers/fixture').write_bytes(b'fixture cover')
 with sqlite3.connect(root/'library.sqlite') as db:
  db.executescript("CREATE TABLE books(id INTEGER PRIMARY KEY,title TEXT,opaque BLOB);INSERT INTO books VALUES(1,'Fixture',X'0102');CREATE TABLE cover_assets(local_path TEXT);INSERT INTO cover_assets VALUES('fixture');CREATE TABLE staff_sessions(token TEXT);INSERT INTO staff_sessions VALUES('private fixture token')")
 before=module.fingerprint(root)
 (root/'covers/derived').mkdir();(root/'covers/derived/fixture-thumbnail.jpg').write_bytes(b'rebuildable thumbnail')
 module.compare(root,before)
 (root/'covers/derived/fixture-thumbnail.jpg').write_bytes(b'changed cache')
 module.compare(root,before)
 with sqlite3.connect(root/'library.sqlite') as db:
  db.executescript("ALTER TABLE books ADD COLUMN new_column TEXT;CREATE TABLE member_registrations(id INTEGER);DELETE FROM staff_sessions")
 module.compare(root,before)
 assert 'private fixture token' not in json.dumps(before)
 with sqlite3.connect(root/'library.sqlite') as db:db.execute("UPDATE books SET title='Changed' WHERE id=1")
 try:module.compare(root,before)
 except RuntimeError:pass
 else:raise AssertionError('changed original field accepted')
 with sqlite3.connect(root/'library.sqlite') as db:db.execute("UPDATE books SET title='Fixture' WHERE id=1")
 (root/'covers/fixture').write_bytes(b'changed cover')
 try:module.compare(root,before)
 except RuntimeError:pass
 else:raise AssertionError('changed cover accepted')
 (root/'covers/fixture').unlink()
 try:module.fingerprint(root)
 except ValueError:pass
 else:raise AssertionError('missing referenced cover accepted')
 (root/'covers/fixture').symlink_to(root/'covers/derived/fixture-thumbnail.jpg')
 try:module.fingerprint(root)
 except ValueError:pass
 else:raise AssertionError('referenced cover symlink accepted')
 with sqlite3.connect(root/'library.sqlite') as db:db.execute("UPDATE cover_assets SET local_path='../outside'")
 try:module.fingerprint(root)
 except ValueError:pass
 else:raise AssertionError('referenced cover traversal accepted')
 current=pathlib.Path(__file__).with_name('Caddyfile.lan.example').read_text()
 start=current.index('\n\t\t\t# Staff admin session');end=current.index('\n\t\t\treverse_proxy unix',start)
 legacy=current[:start]+current[end:]
 upgraded=module.gateway_with_staff(legacy.encode()).decode()
 assert upgraded.replace(upgraded[upgraded.index('\n\t\t\t# Require an individual'):upgraded.index('\n\t\t\treverse_proxy unix')],'')==legacy
 assert upgraded.count('/api/auth/authorize-update')==1
 for invalid in [legacy.replace('tls internal',''),legacy.replace('https://library.local {','https://elsewhere.invalid {'),upgraded]:
  try:module.gateway_with_staff(invalid.encode())
  except ValueError:pass
  else:raise AssertionError('unsupported gateway accepted')
 source=pathlib.Path(__file__).with_name('deploy-staff-release.py').read_text()
 assert source.index("snapshot=private_backups/'snapshot'")<source.index("system('stop',*services)")<source.index("write_private(GATEWAY")
 assert "manifest['commit']!=args.commit" in source and "manifest['version']!=args.version" in source
 assert "proof.get('sha256')!=digest" in source
 assert "UnsetEnvironment=LIBRARY_WALLET_CONFIG LIBRARY_PUBLIC_URL" in source
 print('PASS: referenced-original cover audit ignores derived caches; missing/unsafe originals rejected; private canonical fields; additive migration compatibility; gateway preserves existing bytes; backup/proof before privileged changes; pinned release identity and disabled Wallet.')
