#!/usr/bin/env python3
"""Collect exact-edition subtitles, then apply a private, restartable fill-only plan."""
import argparse,json,os,pathlib,re,time,urllib.request,urllib.parse

class Redirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,req,fp,code,msg,headers,newurl):
        u=urllib.parse.urlsplit(newurl)
        if req.host!='openlibrary.org' or u.scheme!='https' or u.netloc!='openlibrary.org' or u.query or not re.fullmatch(r'/books/OL[0-9]+M\.json',u.path):
            raise ValueError('Unexpected redirect')
        return super().redirect_request(req,fp,code,msg,headers,newurl)

opener=urllib.request.build_opener(Redirects())
def request(url,body=None):
    headers={'User-Agent':'HomeLibrary/0.4 (personal local catalogue)'}
    if body is not None:
        origin=urllib.parse.urlsplit(url)
        headers.update({'Content-Type':'application/json','Origin':origin.scheme+'://'+origin.netloc})
    req=urllib.request.Request(url,data=None if body is None else json.dumps(body).encode(),headers=headers)
    with opener.open(req,timeout=300 if body is not None else 20) as r:
        limit=(1<<20) if req.host=='openlibrary.org' else (8<<20)
        raw=r.read(limit+1)
        if len(raw)>limit: raise ValueError('Oversized response')
        return json.loads(raw)

def subtitle(book):
    isbn=book.get('isbn','');edition=book.get('source_id','')
    if re.fullmatch(r'[0-9]{13}',isbn):
        data=request('https://openlibrary.org/api/books?'+urllib.parse.urlencode({'bibkeys':'ISBN:'+isbn,'jscmd':'data','format':'json'})).get('ISBN:'+isbn,{})
    elif re.fullmatch(r'OL[0-9]+M',edition):
        data=request('https://openlibrary.org/books/'+edition+'.json')
    else: return ''
    value=data.get('subtitle','')
    if not isinstance(value,str) or len(value.encode())>1000: return ''
    return value.strip()

def catalogue(base):
    books=[];page=1
    while True:
        data=request(base+'/api/books?page='+str(page));books+=data['books']
        if len(books)>=data['total']:break
        page+=1
    books+=request(base+'/api/deleted-books')
    return books

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base',default='http://127.0.0.1:8080')
    parser.add_argument('--plan',type=pathlib.Path,required=True)
    parser.add_argument('--apply',action='store_true')
    args=parser.parse_args();base=args.base.rstrip('/');url=urllib.parse.urlsplit(base)
    if url.scheme!='http' or url.hostname not in ('127.0.0.1','localhost') or url.username or url.password or url.path or url.query or url.fragment:parser.error('Use the private loopback library address')
    os.umask(0o077)
    if not args.apply:
        if args.plan.exists():parser.error('Plan already exists; use a new path or apply the existing plan')
        books=catalogue(base);updates=[];missing=errors=existing=0
        for index,b in enumerate(books):
            if b.get('subtitle','').strip():existing+=1;continue
            if not b.get('isbn') and not b.get('source_id'):missing+=1;continue
            try:
                value=subtitle(b)
                if value:updates.append({k:b[k] for k in ('id','isbn','source_id','revision')}|{'subtitle':value})
                else:missing+=1
            except Exception:errors+=1
            time.sleep(1.1)
            if (index+1)%20==0:print(json.dumps({'checked':index+1,'found':len(updates),'lookup_errors':errors}),flush=True)
        plan={'format':1,'books':books,'updates':updates,'missing':missing,'lookup_errors':errors,'existing':existing}
        with args.plan.open('x') as f:json.dump(plan,f);f.flush();os.fsync(f.fileno())
        print(json.dumps({'dry_run':True,'books':len(books),'subtitles_found':len(updates),'existing':existing,'no_subtitle_or_identity':missing,'lookup_errors':errors}),flush=True)
    else:
        if args.plan.stat().st_size>16<<20:raise ValueError('Oversized plan')
        plan=json.loads(args.plan.read_text());assert plan['format']==1
        changed=skipped=0
        for offset in range(0,len(plan['updates']),20):
            result=request(base+'/api/subtitles/backfill',{'updates':plan['updates'][offset:offset+20]})
            changed+=result['changed'];skipped+=result['skipped']
        result=request(base+'/api/subtitles/publish',{})
        if not result.get('published'):raise RuntimeError('Subtitles saved; publication needs retry')
        current={b['id']:b for b in catalogue(base)}
        # Migration may change only subtitle and book revision, never catalogue identity or inventory.
        fields=('id','isbn','title','authors','publisher','publication_date','source_url','source_id','copies','deleted_at')
        preserved=all(b['id'] in current and all(current[b['id']].get(k)==b.get(k) for k in fields) for b in plan['books'])
        filled=sum(bool(current[u['id']].get('subtitle')) for u in plan['updates'] if u['id'] in current)
        print(json.dumps({'applied':True,'changed':changed,'skipped_existing_or_changed':skipped,'planned_records_with_subtitles':filled,'original_metadata_and_inventory_preserved':preserved,'published':True}),flush=True)
        if not preserved:raise RuntimeError('Other catalogue changes detected; inspect concurrent edits before claiming preservation')
if __name__=='__main__':main()
