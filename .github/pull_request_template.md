## Summary
<!-- Briefly describe what this PR does. -->
<!-- 이 PR이 무엇을 하는지 간단히 요약해주세요. -->
- CI workflow 에서 문서 제외, 문서가 들어가도 ci 가 도는데 이거 제외하도록 추가해줌.    
- test-e2e.yml 같은 경우 비용이 좀 있어서 추가해주긴 했지만, 살펴봐야 함.  
- test-e2e.yml 이거는 어떻게 줄일지 고민해봐야 함.  
- readme 파일에서 ./bin/golangci-lint 를 사용하도록 언급함.  
- ci 에서는 2.x 버전을 사용하고 있는데 현재 내 로컬은 1.x 버전이었다. 충돌 이나 린트가 혼용될 수 있어서 로컬과 맞춰야 하는데 ./bin/golangci-lint 는 버전이 맞았다.  
- ci 에서 보통 go mod tidy 빼야 바람직하지만, 로컬에서 tidy 하도록 하고 만약 mod/sum 파일이 차이가 있으면 실패하도록 하였음.
- 물론 pr 이 깨질 수 있지만, 로컬에서 go mod tidy 하고 ci 하도록 강제함.


## Motivation / Context
<!-- Why is this change needed? What problem does it solve? -->
<!-- Why 지금 이 변경이 필요한지, 어떤 문제를 해결하는지 설명해주세요. -->
<!-- Link related issues if any. -->
<!-- 관련 이슈가 있다면 링크해주세요. -->

## Changes
<!-- List the main changes in this PR. -->
<!-- 이 PR에서 변경된 주요 내용을 나열해주세요. -->
- 

## Behavior Changes
<!-- Describe any user-visible or CI behavior changes. -->
<!-- 사용자 관점 또는 CI 동작 관점에서 변경된 점이 있다면 설명해주세요. -->
- 

## Verification
<!-- How was this change tested or verified? -->
<!-- 이 변경을 어떻게 검증했는지 체크해주세요. -->
- [ ] Local tests (로컬 테스트)
- [ ] CI workflows (CI 워크플로 확인)
- [ ] Manual verification (수동 검증, 해당 시)

## Notes
<!-- Optional: anything reviewers should be aware of -->
<!-- 리뷰어가 알아두면 좋은 추가 정보가 있다면 적어주세요. -->
- readme 에서는 go version v1.24+ 이렇게 되어 있는데, ci 테스트 에서 go version v1.22+ 이렇게 작성되어서 혼선 발생함.  
- 어떻게 할지 결정해야함.  
