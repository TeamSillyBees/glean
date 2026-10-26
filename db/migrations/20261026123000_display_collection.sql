-- Additive rendering projection from the already-delivered LLM HTML collection function.
-- Original raw evidence, scientific classification and stable identities remain untouched.
UPDATE "glean_membership"
SET "dimensions" = "dimensions" || jsonb_build_object('display_collection', jsonb_build_array(
 CASE
 WHEN "raw"->>'publication_eligibility' = '符合' AND "raw"->>'topic_status' = '已核相关或参考' THEN '正式纳入'
 WHEN CASE
   WHEN "raw" ? 'year' AND ("raw"->'year' = 'null'::jsonb OR btrim("raw"->>'year') = '' OR "raw"->'year' = 'false'::jsonb) THEN true
   WHEN "raw"->'year' = 'true'::jsonb THEN true
   WHEN "raw"->>'year' ~ '^[+-]?([0-9]+([.][0-9]*)?|[.][0-9]+)([eE][+-]?[0-9]+)?$' THEN ("raw"->>'year')::numeric < 2025
   ELSE false END THEN '窗口外背景'
 WHEN position('预印本' in coalesce("raw"->>'status','')) > 0 THEN '预印本候选'
 WHEN "raw"->>'publication_eligibility' = '特殊分轨' THEN '特殊分轨'
 WHEN "raw"->>'publication_eligibility' = '条件外' OR "raw"->>'topic_status' = '范围外／方法对照' THEN '条件外或方法对照'
 ELSE '待核实' END))
WHERE "topic" = 'llm-agent' AND NOT ("dimensions" ? 'display_collection');
