#### TODO
~~- ./hack/dev-start.sh 오탐하는 것 해결해야함. wrapper 나오는 부분 해결했는데 다시 문제남.~~
- 네임스페이스 지금 default 로 쓰는데 정하고 통일 해야함.
~~- instrument 하고 recorder 좀 겹치는 느낌이 있음.~~
- method 로 help 관련 옮기기  
- a_metrics_text_delta.go 이거 일단 살리는 방향으로..  

### 실행할때 (붙여넣기용)
export E2E_SKIP_CLEANUP=1
export ARTIFACTS_DIR=/tmp/slo-artifacts
export SLOLAB_ENABLED=1
export CI_RUN_ID=local-$(date +%s)

make test-e2e


