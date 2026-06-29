# 📱 DevRemote (가칭) 프로젝트 마스터 문서

본 디렉토리의 문서들은 "AI 코딩 에이전트를 모바일에서 원격으로 감시하고 제어(Human-In-The-Loop)하는 전용 커맨드 센터" 앱, **DevRemote**의 기술 및 비즈니스 청사진을 담고 있습니다.

어떤 상황에서도 프로젝트의 히스토리와 결정된 아키텍처 방향을 잃지 않고 바로 팔업(Follow-up)할 수 있도록, 가장 최신의 심층 논의(보안, 라이선스, 아키텍처 피봇)를 모두 반영하여 작성되었습니다.

## 📑 마스터 문서 목차

### [01. 프로젝트 개요 (Project Overview)](file:///Users/mhk/.gemini/antigravity/brain/14e337a1-787e-4ec1-a00b-0108fbd60e7d/01-project-overview.md)
* 프로젝트 비전 및 해결하고자 하는 핵심 문제 (Pain Points)
* 3중 보안 방어 체계 및 공격 시나리오별 대응
* 경쟁자를 압도하는 '오픈소스 해자(Moat)' 전략

### [02. 네트워크 아키텍처 (Network Architecture)](file:///Users/mhk/.gemini/antigravity/brain/14e337a1-787e-4ec1-a00b-0108fbd60e7d/02-network-architecture.md)
* Tailscale을 배제한 $0 서버리스 1:1 다이렉트 연결 (WebRTC / UPnP / 홀 펀칭)
* UPnP 실패(이중 NAT 등) 현실 점검 및 P2P 성공률(90%) / Relay(10%) 보수적 산정
* mTLS 상호 인증 및 Firebase 시그널링 구조

### [03. 인프라스트럭처 비용 (Infrastructure Cost)](file:///Users/mhk/.gemini/antigravity/brain/14e337a1-787e-4ec1-a00b-0108fbd60e7d/03-infrastructure-cost.md)
* Firebase Security Rules 설정 및 E2EE IP 저장 비용 최적화 ($0 달성)
* 최악의 상황을 가정한 릴레이(Relay) 서버 트래픽 비용 재계산
* 향후 예상되는 고객 지원(CS) 부담 추정

### [04. 마스터 구현 계획 (Master Implementation Plan)](file:///Users/mhk/.gemini/antigravity/brain/14e337a1-787e-4ec1-a00b-0108fbd60e7d/04-master-implementation-plan.md)
* 단계별 개발 로드맵 (Phase 1: 모바일 UX → Phase 2: WebRTC & 데몬 → Phase 3: 상용화)
* 터미널 제어 접근법 (기존 창 가로채기가 아닌 자체 PTY 생성 방식)
* Firebase Auth 통합 및 mTLS 보안 강화 태스크

### [05. 라이선스 및 비즈니스 모델 (License & Business Model)](file:///Users/mhk/.gemini/antigravity/brain/14e337a1-787e-4ec1-a00b-0108fbd60e7d/05-license-business-model.md)
* AGPL-3.0 리스크 극복을 위한 완전 자체 개발(Clean Room) 선언
* GitLab 모델을 벤치마킹한 오픈 코어(Open Core) 비즈니스 모델
* Firebase API 키 탈취 리스크 방어 전략

### [06. 경쟁 환경 분석 (Competitive Landscape)](file:///Users/mhk/.gemini/antigravity/brain/14e337a1-787e-4ec1-a00b-0108fbd60e7d/06-competitive-landscape.md)
* 기존 우회망(Tailscale + RDP/SSH)과의 정면 비교 및 압도적 우위 입증
* 솔직한 약점(Weakness) 인정 및 완화(Mitigation) 전략
* 코딩 에이전트 시장 폭발에 따른 모바일 모니터링 선점 우위 분석

### [07. 시장 동향 및 리서치 (Market Research)](file:///Users/mhk/.gemini/antigravity/brain/14e337a1-787e-4ec1-a00b-0108fbd60e7d/07-market-research.md)
* 코딩 에이전트 전용 원격 제어 및 HITL(Human-In-The-Loop) 모바일 관제 시장 분석
* 해결하고자 하는 킬러 페인포인트 및 떠오르는 스타트업 동향
* API 기반 대시보드 vs 하이브리드(DevRemote) 접근법 차별화 우위
