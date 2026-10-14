import importlib.util,pathlib,unittest,urllib.request
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('backfill',pathlib.Path(__file__).with_name('backfill-subtitles.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class MigrationTests(unittest.TestCase):
 def test_only_identifiers_leave_catalogue(self):
  b={'isbn':'9780140328721','title':'Private title','notes':'Private note','source_id':'OL1M'}
  with patch.object(m,'request',return_value={'ISBN:9780140328721':{'subtitle':' A subtitle '}}) as req:
   self.assertEqual(m.subtitle(b),'A subtitle')
   url=req.call_args.args[0];self.assertIn('ISBN%3A9780140328721',url);self.assertNotIn('Private',url)
 def test_no_guess_and_bad_provider_values(self):
  with patch.object(m,'request') as req:
   self.assertEqual(m.subtitle({'title':'Title: subtitle'}),'');req.assert_not_called()
  for value in [None,{},'x'*1001]:
   with patch.object(m,'request',return_value={'subtitle':value}):self.assertEqual(m.subtitle({'source_id':'OL1M'}),'')
 def test_reject_redirect_to_another_host_or_local_service(self):
  r=urllib.request.Request('https://openlibrary.org/isbn/9780140328721.json')
  for url in ['https://evil.example/books/OL1M.json','http://127.0.0.1/','https://openlibrary.org/works/OL1W.json','https://openlibrary.org/books/OL1M.json?token=x']:
   with self.assertRaises(ValueError):m.Redirects().redirect_request(r,None,302,'',{},url)
if __name__=='__main__':unittest.main()
