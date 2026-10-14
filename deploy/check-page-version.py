#!/usr/bin/env python3
"""Reject builds whose key pages cannot identify the intended release."""
from html.parser import HTMLParser
import pathlib,sys
class Page(HTMLParser):
 def __init__(self):super().__init__();self.versions=[]
 def handle_starttag(self,tag,attrs):
  attrs=dict(attrs)
  if tag=='meta' and attrs.get('name')=='library-app-version':self.versions.append(attrs.get('content'))
version,root,*names=sys.argv[1:]
assert names,'No pages selected'
for name in names:
 page=Page();page.feed((pathlib.Path(root)/name).read_text())
 assert page.versions==[version],f'{name} does not identify release {version}'
print('PASS: homepage, Settings and scanning pages identify the intended release.')
