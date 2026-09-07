set -u
B=http://localhost:8111
J='Content-Type: application/json'
A='Accept: application/json, text/event-stream'

R2ID=$(curl -s $B/api/researches/R2 | jq -r '.data.research.id')
S=$(curl -s $B/api/researches/$R2ID | jq -r '.data.sections[0].id')

echo "=== section delete, holds a document, no force (ожидаем 409 + счёт) ==="
curl -s -o /tmp/s1 -w '%{http_code} ' -X DELETE $B/api/sections/$S; cat /tmp/s1; echo

echo "=== section delete with force (ожидаем 200) ==="
curl -s -o /tmp/s2 -w '%{http_code} ' -X DELETE "$B/api/sections/$S?force=true"; cat /tmp/s2; echo
echo "документы секции после force:"; curl -s $B/api/researches/$R2ID/entries | jq -c '[.data[]?.code]'
echo "внешние ссылки осиротели?"; curl -s $B/api/researches/$R2ID/links | jq -c '.data | length'

echo
echo "=== MCP: research_delete без confirm ==="
SID=$(curl -s -D - -o /dev/null -X POST $B/mcp -H "$J" -H "$A" -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"p","version":"1"}}}' | grep -i '^mcp-session-id' | tr -d '\r' | awk '{print $2}')
curl -s -o /dev/null -X POST $B/mcp -H "$J" -H "$A" -H "Mcp-Session-Id: $SID" -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'
curl -s -X POST $B/mcp -H "$J" -H "$A" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"research_delete","arguments":{"research_id":"R3"}}}' | sed -n 's/^data: //p' | jq -r '.result.content[0].text'

echo "=== R3 всё ещё на месте? ==="; curl -s -o /dev/null -w '%{http_code}\n' $B/api/researches/R3

echo
echo "=== MCP: research_delete_preview для R3 ==="
curl -s -X POST $B/mcp -H "$J" -H "$A" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"research_delete_preview","arguments":{"research_id":"R3"}}}' | sed -n 's/^data: //p' | jq -r '.result.content[0].text' | head -14

echo "=== MCP: research_delete с confirm ==="
curl -s -X POST $B/mcp -H "$J" -H "$A" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"research_delete","arguments":{"research_id":"R3","confirm":true}}}' | sed -n 's/^data: //p' | jq -r '.result.content[0].text' | head -6
echo "R3 после удаления:"; curl -s -o /dev/null -w '%{http_code}\n' $B/api/researches/R3
