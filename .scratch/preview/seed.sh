set -u
B=http://localhost:8111
J='Content-Type: application/json'

mk_research() { curl -s -X POST $B/api/researches -H "$J" -d "$1"; }

echo "=== R1: a full project, the one to delete ==="
R1=$(mk_research '{"name":"Choosing a search engine","description":"Full fixture","goal":"Decide before rewriting","sections":[{"name":"brief","display_name":"Brief","position":0},{"name":"findings","display_name":"Findings","position":1},{"name":"empty","display_name":"Empty section","position":2}]}')
echo "$R1" | jq -c ".data | {code,research_id,sections_created}"
R1ID=$(echo "$R1" | jq -r .data.research_id)
S1=$(curl -s $B/api/researches/$R1ID | jq -r '.data.sections[0].id')
S3=$(curl -s $B/api/researches/$R1ID | jq -r '.data.sections[2].id')

curl -s -X POST $B/api/entries -H "$J" -d "{\"research_id\":\"$R1ID\",\"section_id\":\"$S1\",\"title\":\"Requirements\",\"content\":\"Typo tolerance matters. See https://meilisearch.com/docs for the reference.\",\"entry_type\":\"markdown\"}" | jq -c '.data | {code,title}'
curl -s -X POST $B/api/entries -H "$J" -d "{\"research_id\":\"$R1ID\",\"section_id\":\"$S1\",\"title\":\"Constraints\",\"content\":\"Budget is fixed, see [[E1]].\",\"entry_type\":\"markdown\"}" | jq -c '.data | {code,title}'
SESS=$(curl -s -X POST $B/api/sessions -H "$J" -d "{\"research_id\":\"$R1ID\",\"title\":\"Kickoff interview\",\"focus\":\"What must be true\"}" | jq -r '.data.id')
curl -s -X POST $B/api/sessions/$SESS/questions -H "$J" -d '{"questions":[{"text":"What breaks today?"},{"text":"Who owns the index?"},{"text":"What is the budget?"}]}' -o /dev/null -w 'questions: %{http_code}\n'
curl -s -X POST $B/api/tasks -H "$J" -d "{\"research_id\":\"$R1ID\",\"title\":\"Benchmark both\"}" -o /dev/null -w 'task: %{http_code}\n'
curl -s -X POST $B/api/researches/$R1ID/shares -H "$J" -d '{}' -o /dev/null -w 'share: %{http_code}\n'

echo
echo "=== R2: cites R1, and must survive the delete with an inert link ==="
R2=$(mk_research '{"name":"Support desk rewrite","description":"The citing project","goal":"Ship the new desk","sections":[{"name":"plan","display_name":"Plan","position":0}]}')
echo "$R2" | jq -c ".data | {code,research_id}"
R2ID=$(echo "$R2" | jq -r .data.research_id)
S2=$(curl -s $B/api/researches/$R2ID | jq -r '.data.sections[0].id')
R1CODE=$(echo "$R1" | jq -r .data.code)
curl -s -X POST $B/api/entries -H "$J" -d "{\"research_id\":\"$R2ID\",\"section_id\":\"$S2\",\"title\":\"Search decision\",\"content\":\"This rests entirely on [[${R1CODE}:E1]] and on [[${R1CODE}]].\",\"entry_type\":\"markdown\"}" | jq -c '.data | {code,title}'

echo
echo "=== R3: empty, for the empty-summary dialog ==="
mk_research '{"name":"Nothing here yet","description":"Empty","goal":"Not started","sections":[]}' | jq -c ".data | {code,research_id}"

echo
echo "=== delete-preview for R1 ==="
curl -s $B/api/researches/$R1ID/delete-preview | jq -c '.data'
echo "empty-section id (for the settings delete): $S3"
