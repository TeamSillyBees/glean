-- Exact cross-source PMID bridge; immutable metadata stays untouched.
-- Fixed JSON key is indexed for PubMed / Europe PMC identity resolution.
CREATE INDEX "gleanobservation_metadata_pmid" ON "glean_observation" (("metadata"->>'pmid'));
CREATE INDEX "gleanobservation_metadata_doi" ON "glean_observation" (("metadata"->>'doi'));
CREATE INDEX "gleanobservation_metadata_arxiv" ON "glean_observation" (("metadata"->>'arxivId'));
