#!/bin/bash
# Comprehensive HTTP API test script for Teammate Server
BASE="http://localhost:8080"
PASS=0
FAIL=0
ERRORS=""

check() {
    local name="$1" method="$2" url="$3" expected="$4" body="$5"
    if [ -n "$body" ]; then
        resp=$(curl -s -w "\n%{http_code}" -X "$method" "$BASE$url" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$body" 2>&1)
    else
        resp=$(curl -s -w "\n%{http_code}" -X "$method" "$BASE$url" \
            -H "Authorization: Bearer $TOKEN" 2>&1)
    fi
    code=$(echo "$resp" | tail -1)
    body_resp=$(echo "$resp" | sed '$d')

    if [ "$code" = "$expected" ]; then
        echo "  PASS [$code] $name"
        PASS=$((PASS+1))
    else
        echo "  FAIL [$code != $expected] $name"
        echo "    Response: $(echo "$body_resp" | head -c 300)"
        FAIL=$((FAIL+1))
        ERRORS="$ERRORS\n  $name: expected $expected, got $code - $(echo "$body_resp" | head -c 100)"
    fi
    # Return body for variable capture
    echo "$body_resp"
}

echo "=========================================="
echo " Teammate Server API Test Suite"
echo "=========================================="

# --- Auth ---
echo ""
echo "--- Auth ---"
REGISTER_RESP=$(curl -s -X POST "$BASE/api/auth/register" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"API Test User\",\"email\":\"apitest_$(date +%s)@test.com\",\"password\":\"test123\"}")
TOKEN=$(echo "$REGISTER_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])" 2>/dev/null)
if [ -z "$TOKEN" ]; then
    echo "FATAL: Register failed"
    echo "$REGISTER_RESP"
    exit 1
fi
echo "  PASS [register + get token]"
PASS=$((PASS+1))

LOGIN_RESP=$(curl -s -X POST "$BASE/api/auth/login" \
    -H "Content-Type: application/json" \
    -d '{"email":"test@test.com","password":"test123"}')
TOKEN=$(echo "$LOGIN_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])" 2>/dev/null)
if [ -z "$TOKEN" ]; then
    echo "FATAL: Login failed"
    exit 1
fi
echo "  PASS [login]"
PASS=$((PASS+1))

check "Whoami" GET "/api/auth/whoami" 200 ''
check "Logout" POST "/api/auth/logout" 200 ''

# Re-login
LOGIN_RESP=$(curl -s -X POST "$BASE/api/auth/login" \
    -H "Content-Type: application/json" \
    -d '{"email":"test@test.com","password":"test123"}')
TOKEN=$(echo "$LOGIN_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])" 2>/dev/null)

# --- Workspaces ---
echo ""
echo "--- Workspaces ---"
WS_LIST=$(curl -s "$BASE/api/workspaces" -H "Authorization: Bearer $TOKEN")
WS_ID=$(echo "$WS_LIST" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])" 2>/dev/null)
echo "  Using workspace: $WS_ID"
check "List Workspaces" GET "/api/workspaces" 200 ''
check "Get Workspace" GET "/api/workspaces/$WS_ID" 200 ''
check "List Members" GET "/api/workspaces/$WS_ID/members" 200 ''

WS_CREATE=$(curl -s -X POST "$BASE/api/workspaces" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"Test WS New","description":"test"}')
NEW_WS_ID=$(echo "$WS_CREATE" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null)
if [ -n "$NEW_WS_ID" ] && [ "$NEW_WS_ID" != "None" ]; then
    echo "  PASS [create workspace $NEW_WS_ID]"
    PASS=$((PASS+1))
    check "Update Workspace (member=403)" PUT "/api/workspaces/$NEW_WS_ID" 403 '{"name":"Updated WS"}'
    check "Delete Workspace (member=403)" DELETE "/api/workspaces/$NEW_WS_ID" 403 ''
else
    echo "  FAIL [create workspace]"
    FAIL=$((FAIL+1))
fi

# --- Agents ---
echo ""
echo "--- Agents ---"
check "List Agents" GET "/api/workspaces/$WS_ID/agents" 200 ''

AGENT_CREATE=$(curl -s -X POST "$BASE/api/workspaces/$WS_ID/agents" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"api-test-agent","provider":"claude","git_name":"API Agent","git_email":"api@test.com"}')
AGENT_ID=$(echo "$AGENT_CREATE" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null)
if [ -n "$AGENT_ID" ] && [ "$AGENT_ID" != "None" ]; then
    echo "  PASS [create agent $AGENT_ID]"
    PASS=$((PASS+1))
    check "Get Agent" GET "/api/workspaces/$WS_ID/agents/$AGENT_ID" 200 ''
    check "Update Agent" PUT "/api/workspaces/$WS_ID/agents/$AGENT_ID" 200 '{"instructions":"test instructions","model":"claude-3"}'
    check "Update Agent Status" PATCH "/api/workspaces/$WS_ID/agents/$AGENT_ID/status" 200 '{"status":"online"}'
    check "Delete Agent" DELETE "/api/workspaces/$WS_ID/agents/$AGENT_ID" 204 ''
else
    echo "  FAIL [create agent]"
    FAIL=$((FAIL+1))
    echo "    Response: $AGENT_CREATE"
fi

# --- Projects ---
echo ""
echo "--- Projects ---"
PROJ_CREATE=$(curl -s -X POST "$BASE/api/workspaces/$WS_ID/projects" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"Test Project","project_key":"TPST","repo_url":"https://github.com/test/repo.git"}')
PROJ_ID=$(echo "$PROJ_CREATE" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null)
if [ -n "$PROJ_ID" ] && [ "$PROJ_ID" != "None" ]; then
    echo "  PASS [create project $PROJ_ID]"
    PASS=$((PASS+1))
    check "Get Project" GET "/api/workspaces/$WS_ID/projects/$PROJ_ID" 200 ''
    check "List Projects" GET "/api/workspaces/$WS_ID/projects" 200 ''
    check "Update Project" PUT "/api/workspaces/$WS_ID/projects/$PROJ_ID" 200 '{"name":"Updated Project"}'
    check "List Project Members" GET "/api/workspaces/$WS_ID/projects/$PROJ_ID/members" 200 ''
else
    echo "  FAIL [create project]"
    FAIL=$((FAIL+1))
    echo "    Response: $PROJ_CREATE"
fi

# --- Workflows ---
echo ""
echo "--- Workflows ---"
WF_CREATE=$(curl -s -X POST "$BASE/api/workspaces/$WS_ID/workflows" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"Test Workflow","description":"test","nodes":[{"name":"Step 1","node_type":"standard","assignee_type":"auto","sort_order":1}]}')
WF_ID=$(echo "$WF_CREATE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id') or d.get('template',{}).get('id',''))" 2>/dev/null)
if [ -n "$WF_ID" ] && [ "$WF_ID" != "None" ]; then
    echo "  PASS [create workflow $WF_ID]"
    PASS=$((PASS+1))
    check "List Workflows" GET "/api/workspaces/$WS_ID/workflows" 200 ''
    check "Get Workflow" GET "/api/workspaces/$WS_ID/workflows/$WF_ID" 200 ''
    check "Delete Workflow" DELETE "/api/workspaces/$WS_ID/workflows/$WF_ID" 204 ''
else
    echo "  FAIL [create workflow]"
    FAIL=$((FAIL+1))
    echo "    Response: $WF_CREATE"
fi

# --- Tasks ---
echo ""
echo "--- Tasks ---"
if [ -n "$PROJ_ID" ] && [ "$PROJ_ID" != "None" ]; then
    WF2_CREATE=$(curl -s -X POST "$BASE/api/workspaces/$WS_ID/workflows" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d '{"name":"Task Workflow","description":"for task test","nodes":[{"name":"Step 1","node_type":"standard","assignee_type":"auto","sort_order":1},{"name":"Review","node_type":"review","assignee_type":"auto","sort_order":2}]}')
    WF2_ID=$(echo "$WF2_CREATE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id') or d.get('template',{}).get('id',''))" 2>/dev/null)
    echo "  WF2_ID=$WF2_ID"

    if [ -n "$WF2_ID" ] && [ "$WF2_ID" != "None" ]; then
        TASK_CREATE=$(curl -s -X POST "$BASE/api/projects/$PROJ_ID/tasks" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "{\"title\":\"Test Task\",\"description\":\"test\",\"workflow_template_id\":\"$WF2_ID\"}")
        TASK_ID=$(echo "$TASK_CREATE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id') or d.get('task',{}).get('id',''))" 2>/dev/null)
        echo "  TASK_ID=$TASK_ID"
        if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "None" ]; then
            echo "  PASS [create task $TASK_ID]"
            PASS=$((PASS+1))
            check "List Tasks" GET "/api/projects/$PROJ_ID/tasks" 200 ''
            check "Get Task" GET "/api/projects/$PROJ_ID/tasks/$TASK_ID" 200 ''
            check "List Task Nodes" GET "/api/tasks/$TASK_ID/nodes" 200 ''
            check "Update Task (cancel)" PUT "/api/projects/$PROJ_ID/tasks/$TASK_ID" 200 '{"status":"cancelled"}'
        else
            echo "  FAIL [create task]"
            FAIL=$((FAIL+1))
            echo "    Response: $(echo "$TASK_CREATE" | head -c 300)"
        fi
    else
        echo "  SKIP [tasks - no workflow]"
    fi
else
    echo "  SKIP [tasks - no project]"
fi

# --- Board ---
echo ""
echo "--- Board ---"
if [ -n "$PROJ_ID" ] && [ "$PROJ_ID" != "None" ]; then
    check "Get Board" GET "/api/projects/$PROJ_ID/board" 200 ''
fi

# --- Skills ---
echo ""
echo "--- Skills ---"
check "List Skills" GET "/api/workspaces/$WS_ID/skills" 200 ''
SKILL_CREATE=$(curl -s -X POST "$BASE/api/workspaces/$WS_ID/skills" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"test-skill","description":"test","context":"test context"}')
SKILL_ID=$(echo "$SKILL_CREATE" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null)
if [ -n "$SKILL_ID" ] && [ "$SKILL_ID" != "None" ]; then
    echo "  PASS [create skill]"
    PASS=$((PASS+1))
    check "Delete Skill" DELETE "/api/workspaces/$WS_ID/skills/$SKILL_ID" 204 ''
fi

# --- MCP Servers ---
echo ""
echo "--- MCP Servers ---"
check "List MCP Servers" GET "/api/workspaces/$WS_ID/mcp-servers" 200 ''

# --- Memories ---
echo ""
echo "--- Memories ---"
# Create an agent first for memory tests
MEM_AGENT_CREATE=$(curl -s -X POST "$BASE/api/workspaces/$WS_ID/agents" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"memory-test-agent","provider":"claude","git_name":"Memory Agent","git_email":"memory@test.com"}')
MEM_AGENT_ID=$(echo "$MEM_AGENT_CREATE" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null)
if [ -n "$MEM_AGENT_ID" ] && [ "$MEM_AGENT_ID" != "None" ]; then
    check "List Memories" GET "/api/memories?agent_id=$MEM_AGENT_ID" 200 ''
    check "Create Memory" POST "/api/memories" 201 '{"content":"test memory","type":"insight","title":"Test Memory","agent_id":"'"$MEM_AGENT_ID"'","project_id":"'"$PROJ_ID"'"}'
    check "Search Memories" GET "/api/memories/search?q=test" 200 ''
    # Cleanup
    curl -s -X DELETE "$BASE/api/workspaces/$WS_ID/agents/$MEM_AGENT_ID" -H "Authorization: Bearer $TOKEN" > /dev/null
else
    check "List Memories" GET "/api/memories?agent_id=00000000-0000-0000-0000-000000000000" 200 ''
    check "Search Memories" GET "/api/memories/search?q=test" 200 ''
fi

# --- Notifications ---
echo ""
echo "--- Notifications ---"
check "List Notifications" GET "/api/workspaces/$WS_ID/notifications" 200 ''

# --- Search ---
echo ""
echo "--- Search ---"
check "Search Tasks" GET "/api/workspaces/$WS_ID/search/tasks?q=test" 200 ''
check "Search Agents" GET "/api/workspaces/$WS_ID/search/agents?q=test" 200 ''

# --- Stats ---
echo ""
echo "--- Stats ---"
if [ -n "$PROJ_ID" ] && [ "$PROJ_ID" != "None" ]; then
    check "Project Stats" GET "/api/projects/$PROJ_ID/stats" 200 ''
fi

# --- Comments ---
echo ""
echo "--- Comments ---"
if [ -n "$TASK_ID" ] && [ "$TASK_ID" != "None" ]; then
    check "Create Comment" POST "/api/tasks/$TASK_ID/comments" 201 '{"content":"test comment"}'
    check "List Comments" GET "/api/tasks/$TASK_ID/comments" 200 ''
fi

# --- Token Exchange ---
echo ""
echo "--- Token Exchange ---"
check "Token Exchange (invalid)" POST "/api/auth/token-exchange" 400 '{"api_token":"invalid"}'
check "Token Exchange (wrong prefix)" POST "/api/auth/token-exchange" 400 '{"api_token":"wrong_prefix_token"}'

# --- Health ---
echo ""
echo "--- Health ---"
check "Health Check" GET "/health" 200 ''

# --- Cleanup ---
echo ""
echo "--- Cleanup ---"
if [ -n "$PROJ_ID" ] && [ "$PROJ_ID" != "None" ]; then
    check "Delete Project" DELETE "/api/workspaces/$WS_ID/projects/$PROJ_ID" 204 ''
fi

# --- Summary ---
echo ""
echo "=========================================="
echo " Results: $PASS passed, $FAIL failed"
echo "=========================================="
if [ $FAIL -gt 0 ]; then
    echo -e "Failed tests:$ERRORS"
    exit 1
fi
