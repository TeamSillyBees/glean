#!/usr/bin/env python3
"""Compile discovery-only targets from immutable curated baseline evidence; no network."""
import argparse, collections, gzip, hashlib, json, pathlib, re
ROOT = pathlib.Path(__file__).resolve().parents[2]
BASE = ROOT / 'data/baselines'
OUT = ROOT / 'data/journals'
def digest(value):
    return hashlib.sha256(value).hexdigest()
def encode(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':')) + '\n').encode()
def normalize_issn(value):
    if not isinstance(value, str): return None
    value = value.strip().upper().replace('-', '')
    if not re.fullmatch(r'[0-9]{7}[0-9X]', value): return None
    if (sum(int(value[i]) * (8-i) for i in range(7)) + (10 if value[7] == 'X' else int(value[7]))) % 11: return None
    return value[:4] + '-' + value[4:]
def as_list(value):
    return value if isinstance(value, list) else [value] if isinstance(value, str) else []
def identity(raw):
    found, paths = set(), set()
    for parent, obj in [('',raw), ('venue_qualification_evidence.',raw.get('venue_qualification_evidence')), ('venue_metric_evidence.',raw.get('venue_metric_evidence'))]:
        if not isinstance(obj, dict): continue
        for key in ('journal_issns','journal_issns_verified','issn','issns','eissn'):
            for value in as_list(obj.get(key)):
                normalized = normalize_issn(value)
                if normalized: found.add(normalized); paths.add(parent+key)
    return sorted(found), sorted(paths)
def title_key(value):
    # Only case/whitespace and a trailing bibliographic full stop; never fuzzy title similarity.
    return ' '.join(str(value).split()).rstrip('.').strip().casefold()
def curated(r):
    d, raw = r.get('dimensions',{}), r.get('raw',{})
    if r['topic']=='biomed-ai': return '正式纳入' in d.get('collection',[])
    if r['topic']=='biomed-worldmodel': return '正式纳入' in d.get('eligibility_status',[])
    return raw.get('publication_eligibility')=='符合' and raw.get('topic_status')=='已核相关或参考'
def venue_kind(r):
    raw=r.get('raw',{}); nested=raw.get('venue_qualification_evidence')
    nested=nested if isinstance(nested,dict) else {}
    explicit=[str(raw.get(k,'')) for k in ('venue_kind','verified_venue_kind','track')]+[str(nested.get('kind','')), str(r.get('publicationType',''))]
    text=' | '.join(explicit).lower()
    if raw.get('is_conference') is True or raw.get('pmlr_volume') or any(x in text for x in ('conference','proceedings-article','main research proceedings','workshop','主会','会议')):
        return 'conference-series', 'explicit conference/series marker (never a JIF-journal target)'
    if raw.get('is_conference') is False or 'journal' in text or '期刊' in text:
        return 'journal','explicit journal/track marker'
    scope=' '.join(str(x) for key in ('scope','display_scope') for x in r.get('dimensions',{}).get(key,[]))
    if '期刊' in scope or 'journal' in scope.lower(): return 'journal','original journal scope label'
    return 'unknown','venue kind unresolved; no name-based journal or metric inference'
def metric_fact(raw):
    nested=raw.get('venue_qualification_evidence')
    nested=nested if isinstance(nested,dict) else {}
    def first(key, *aliases):
        for obj in (raw,nested):
            for name in (key,)+aliases:
                value=obj.get(name)
                if value is not None and value!='': return value
        return None
    value=first('jif'); year=first('jif_year')
    return {'jif':value if isinstance(value,(int,float)) and not isinstance(value,bool) else None,
            'metricYear':year if isinstance(year,int) and not isinstance(year,bool) and year>0 else 0,
            'source':first('metric_url','jif_url','landing_url') or '',
            'originalQualificationVerified':nested.get('qualification_verified') is True or raw.get('venue_qualification_verified') is True,
            'metricYearStatus':raw.get('metric_year_status') or '',
            'sourceKind':nested.get('source_kind') or '',
            'sourceProvenanceCaution':nested.get('source_provenance_caution') or '',
            'correctionPrecedence':nested.get('correction_precedence') or '',
            'metricSourceSHA256':nested.get('source_sha256') or '',
            'yearEvidence':nested.get('year_evidence') or '',
            'originalQualificationEvidence':raw.get('venue_qualification_evidence') if isinstance(raw.get('venue_qualification_evidence'),str) else ''}
def compile_registry():
    manifest_body=(BASE/'manifest.json').read_bytes(); manifest=json.loads(manifest_body)
    seed_body=(OUT/'historical-seed.json').read_bytes(); seeds=json.loads(seed_body)
    groups={}; totals=collections.Counter(); exclusions=collections.Counter(); exclusion_examples={}; covered=collections.Counter(); missing=collections.Counter()
    records_digest=hashlib.sha256(); eligible_proofs=[]; missing_records=[]
    for shard in manifest['shards']:
        if pathlib.Path(shard['file']).name!=shard['file']: raise ValueError('invalid shard path')
        body=(BASE/shard['file']).read_bytes()
        expected=shard.get('sha256') or shard.get('compressedSha256')
        if expected and digest(body)!=expected: raise ValueError('baseline shard checksum mismatch: '+shard['file'])
        shard_hash=digest(body)
        for line in gzip.decompress(body).splitlines():
            r=json.loads(line)
            if not curated(r): continue
            topic=r['topic']; totals[topic]+=1
            issns, paths=identity(r['raw']); kind, reason=venue_kind(r)
            proof={'topic':topic,'originalId':r['originalId'],'doi':r.get('doi',''),'url':r.get('url',''),'venue':r['venue'],'issns':issns,'issnPaths':paths,'kind':kind,'kindEvidence':reason,'shard':shard['file'],'shardSHA256':shard_hash}
            if kind!='journal':
                key=(topic,r['venue'],reason); exclusions[key]+=1; exclusion_examples.setdefault(key,proof); continue
            if not issns:
                missing[topic]+=1; key=(topic,r['venue'],'explicit journal lacks a checksum-valid recorded ISSN');exclusions[key]+=1;exclusion_examples.setdefault(key,proof);missing_records.append((proof,metric_fact(r['raw'])));continue
            covered[topic]+=1; records_digest.update(encode(proof)); eligible_proofs.append(proof)
            key=(r['venue'].strip().casefold(),tuple(issns)); group=groups.setdefault(key,{'name':r['venue'].strip(),'issns':issns,'topics':set(),'proofs':[],'facts':{}})
            group['topics'].add(topic);group['proofs'].append(proof);fact=metric_fact(r['raw']);group['facts'][encode(fact)]=fact
    # Repair only missing topic routes through an already evidenced exact issuer name.
    # Copy identifiers, never JIF/year/qualification from the matched identity.
    lookup=collections.defaultdict(list)
    for seed in seeds:
        if not seed.get('issns'): continue
        entry={'issns':set(seed['issns']),'topics':set(seed.get('eligibleTopics',[])), 'name':seed['name'], 'key':None,
               'proof':{'registryVersion':seed['registryVersion'],'registryName':seed['name'],'sourceSHA256':seed['evidence'].get('sourceSHA256'),'issns':seed['issns']}}
        for name in [seed['name']]+seed.get('aliases',[]):lookup[title_key(name)].append(entry)
    for key,g in list(groups.items()):
        entry={'issns':set(g['issns']),'topics':set(g['topics']), 'name':g['name'],'key':key,
               'proof':{'registryName':g['name'],'issns':g['issns'],'primaryIssuerRecord':g['proofs'][0]}}
        lookup[title_key(g['name'])].append(entry)
    repairs=[]; repair_counts=collections.Counter(); ambiguous=[]
    for original,record_metric in missing_records:
        candidates=lookup.get(title_key(original['venue']),[])
        if not candidates or any(original['topic'] in c['topics'] for c in candidates):continue
        # A name shared by disjoint issuer identities is not an exact identity match.
        united=set(candidates[0]['issns']); remaining=list(candidates[1:])
        while remaining:
            connected=[c for c in remaining if united.intersection(c['issns'])]
            if not connected:break
            for c in connected:united.update(c['issns']);remaining.remove(c)
        if remaining:
            ambiguous.append(original);continue
        ordered=sorted(candidates,key=lambda c:(-len(c['issns']),c['name']))
        existing=next((c for c in ordered if c['key'] is not None and c['issns']==united),None)
        if existing: group=groups[existing['key']]
        else:
            name=ordered[0]['name']; key=(name.casefold(),tuple(sorted(united)))
            group=groups.setdefault(key,{'name':name,'issns':sorted(united),'topics':set(),'proofs':[],'facts':{}})
        proof=dict(original)
        proof['issns']=sorted(united)
        proof['identityResolution']={'basis':'exact preexisting registry name or explicitly verified seed alias; identifiers only',
          'normalization':'case, whitespace, trailing bibliographic full stop only','originalRecordISSNs':[],
          'matchedIdentityEvidence':[c['proof'] for c in ordered], 'originalRecordMetricFacts':record_metric}
        group['topics'].add(original['topic']);group['proofs'].append(proof)
        # Do not project metrics from an identity-resolved record into the existing target.
        repairs.append(proof);repair_counts[original['topic']]+=1
    targets=[]
    for key,g in sorted(groups.items()):
        proofs=sorted(g['proofs'],key=lambda x:(x['topic'],x['originalId'])); facts=[v for _,v in sorted(g['facts'].items())]
        numbers={f['jif'] for f in facts if f['jif'] is not None}; years={f['metricYear'] for f in facts}
        # Conflicting or absent years remain unknown; historical JIF never means fresh qualification.
        jif=next(iter(numbers)) if len(numbers)==1 else None
        year=next(iter(years)) if len(years)==1 else 0
        examples=[]
        for topic in sorted(g['topics']): examples.append(next(p for p in proofs if p['topic']==topic))
        evidence={'discoveryEnabled':True,'discoveryTopics':sorted(g['topics']),'discoveryBasis':'actual historical curated journal memberships; relevance of new papers remains pending',
          'metricVerified':False,'currentQualificationStatus':'pending','sourceTopics':sorted(g['topics']),'sourceSHA256':digest(manifest_body),
          'baselineManifestSHA256':digest(manifest_body),'canonicalMetadataSHA256':digest(encode({'proofs':proofs,'historicalMetricFacts':facts})),
          'membershipCount':len(proofs),'evidenceExamples':examples,'historicalMetricFacts':facts,'compiler':'curated-journal-discovery-v1'}
        target={'name':g['name'],'kind':'journal','issns':g['issns'],'aliases':[],'jif':jif,'ccf':'','metricYear':year,'source':'data/baselines/manifest.json#sha256='+digest(manifest_body),
          'evidence':evidence,'eligibleTopics':[],'qualificationStatus':'historical-curated-discovery'}
        target['registryVersion']='curated-discovery-20261026-'+digest(encode(target))[:16]; targets.append(target)
    registry=seeds+targets; encoded=encode(registry)
    target_index={(v['name'].casefold(), topic, tuple(v['issns'])) for v in targets for topic in v['evidence']['discoveryTopics']}
    unexplained=[p for p in eligible_proofs if (p['venue'].strip().casefold(),p['topic'],tuple(p['issns'])) not in target_index]
    if unexplained: raise ValueError('uncovered curated journal identities: '+repr(unexplained[:3]))
    coverage={'version':1,'baselineManifestSHA256':digest(manifest_body),'historicalSeedSHA256':digest(seed_body),'curatedMemberships':dict(totals),
      'curatedJournalMembershipsWithRecordedValidISSNs':dict(covered),'journalMembershipsWithoutRecordedValidISSNs':dict(missing),
      'knownRegistryNameTopicRepairs':dict(repair_counts),'resolvedTopicRouteRecords':repairs,'ambiguousKnownNameIdentities':ambiguous,'discoveryTargetEntries':len(targets),'coveredRecordsSHA256':records_digest.hexdigest(),'unexplainedCuratedJournalWithValidISSNMisses':len(unexplained),
      'explicitExclusions':[dict(exclusion_examples[k],reason=k[2],memberships=n) for k,n in sorted(exclusions.items())],
      'note':'Conference/series are not JIF-journal targets. Unknown identities remain explicit. No fuzzy issuer merge, inherited current JIF, fabricated metric year, or new scientific inclusion. Exact existing issuer identity may restore discovery routes without filling record-local ISSNs.'}
    result_manifest={'version':1,'sourceArtifact':'historical-seed.json + all committed curated baseline memberships','sourceSHA256':digest(manifest_body),
      'registrySHA256':digest(encoded),'entries':len(registry),'historicalSeedEntries':len(seeds),'curatedDiscoveryEntries':len(targets),'knownRegistryNameTopicRepairs':dict(repair_counts),
      'withValidISSNs':sum(bool(v['issns']) for v in registry),'missingISSNs':sum(not v['issns'] for v in registry),
      'discoveryTopics':{topic:sum(topic in v['evidence']['discoveryTopics'] for v in targets) for topic in sorted(totals)},'coverageSHA256':digest(encode(coverage)),
      'note':'Discovery targets are separate from current metric qualification. Historical curated entries may have metricYear=0; they are enabled for discovery only, never automatically approve new papers.'}
    return {'registry.json':encoded,'manifest.json':encode(result_manifest),'coverage.json':encode(coverage)}
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--check',action='store_true');args=parser.parse_args()
    output=compile_registry()
    for name,body in output.items():
        if args.check:
            if (OUT/name).read_bytes()!=body: raise SystemExit('registry out of date: '+name)
        else:(OUT/name).write_bytes(body)
    print(json.dumps(json.loads(output['manifest.json']),ensure_ascii=False,indent=2))
if __name__=='__main__':main()
