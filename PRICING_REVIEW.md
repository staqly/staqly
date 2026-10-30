# Pricing staqly — analiza i decyzje, 30 października 2026

Ceny pozostają bez zmian: €15 / €29 / €59 / €99 za osobę miesięcznie; rocznie €144 / €276 / €564 / €948.
Limity są wspólne dla workspace’u, a nie mnożone przez liczbę osób. Poniższe liczby to propozycja wdrożona w tym worktree, nie zmiana już opublikowana w produkcji.

## Nowa oferta

| Wspólny limit workspace’u | Starter | Plus | Pro | Business |
|---|---:|---:|---:|---:|
| Połączone kanały | 5 | 15 | 40 | 100 |
| Nowe posty / miesiąc UTC, w tym szkice i duplikaty | 300 | 1 500 | 5 000 | 15 000 |
| Kredyty publikacji X / miesiąc | 50 | 100 | 250 | 500 |
| Świeże odczyty statystyk pojedynczego posta X / miesiąc | 100 | 200 | 500 | 1 000 |
| Posty LinkedIn / miesiąc | 100 | 300 | 600 | 900 |
| W tym posty LinkedIn z wideo | 5 | 15 | 30 | 60 |
| Budżet AI / miesiąc, szacowany koszt dostawców w USD | $1 | $2 | $5 | $10 |
| Biblioteka plików | 2 GB | 10 GB | 30 GB | 100 GB |
| Maksymalny plik | 25 MB | 100 MB | 100 MB | 100 MB |
| Alerty / webhooki / klucze API | 3 / 3 / 2 | 10 / 10 / 10 | 25 / 25 / 25 | 50 / 50 / 50 |
| Eksport danych wraz z plikami i kopie zapasowe | — | Tak | Tak | Tak |

Każdy plan: kalendarz, kolejki i harmonogramy, edytory grafiki, wideo i audio, Analytics, statystyki wspieranych platform, role zespołu, asystent AI, API, MCP, SDK i CLI. Kolekcje i harmonogramy bez limitu liczby; tworzone przez nie posty zużywają limit postów. Agent zewnętrzny nie jest płatnym miejscem, ale jego abonament nie jest finansowany przez staqly.

Jeden post wysłany na kilka kanałów liczy się raz jako utworzony post, lecz osobno na każdym celu X/LinkedIn. Kredyt X nie oznacza zawsze jednej publikacji: aktywny link kosztuje 14 kredytów, każda odpowiedź w wątku też jest rozliczana. Roczny abonament nie daje całej rocznej puli od razu. Niewykorzystane pule miesięczne nie przechodzą dalej.

Przyszłe funkcje są wyraźnie oddzielone od dostępnych: automatyczne podsumowania wyników i rekomendacje AI (Plus+), cykliczne raporty (Pro+), eksport raportów dla klientów (Business). To planowany podział, bez terminu i bez obietnicy dostępności przy zakupie. Przyszłe AI ma korzystać z tej samej puli. Nie oznaczamy istniejącej strony Analytics jako „Coming soon”.

## Co było niezgodne z produktem lub niebezpieczne kosztowo

- Publiczna strona staqly podawała „Every plan has everything”, choć Starter nie obejmuje kopii zapasowych z plikami.
- Każdy płatny plan miał `AIMonthlyCents: Unlimited`; zapasowy model asystenta celowo omijał limit. Rezerwacja rozmowy wynosiła zaledwie $0.01 niezależnie od modelu i kontekstu, bez limitu wyjścia.
- Business oferował nielimitowane kanały i 1 TB miejsca. Nie było skończonej granicy aktywnej konfiguracji automatyzacji w dwóch najwyższych planach.
- Limity były workspace’owe, mimo ceny za osobę; komunikacja nie wyjaśniała, że kolejne miejsca nie zwiększają puli.
- Brakowało edytorów i istniejącego Analytics w porównaniu. Nie ma zweryfikowanego w kodzie poziomu SLA/priorytetu wsparcia, więc usunięto tę obietnicę.
- Reklamowane 250 MB / 1 GB nie przechodziły przez domyślny limit serwera 100 MB i proxy dashboardu 110 MB. Oferta teraz podaje 100 MB; większe uploady wymagają osobnej, spójnej zmiany serwera i proxy.
- Miesięczna pula LinkedIn nie chroniła dziennej puli aplikacji. Również YouTube ma wspólny limit projektu.
- Odczyty statystyk X mają koszt niezależny od publikacji. Samo ograniczenie publikacji nie zamykało tego wydatku.

Dowody: `apps/api/internal/app/plans.go`, `assistant.go`, `ai.go`, `ai_catalog.go`, `credits.go`, `post_share.go`, `analytics_report.go`; `apps/api/internal/web/web.go`; `apps/platform/components/analytics`; publiczna strona <https://www.staqly.ai/pricing/>.

## Konkurencja — odczytane publiczne cenniki

Waluty i modele rozliczenia pozostawione jak w źródłach. Cena za markę, kanał i osobę nie jest tym samym. Nie opieramy oferty na czasowych promocjach.

| Serwis | Odczytana oferta | Wniosek dla staqly |
|---|---|---|
| [Buffer](https://buffer.com/pricing) | Essentials $5/kanał/mies. rocznie, Team $10/kanał/mies. rocznie; Team obejmuje nielimitowanych współpracowników. AI i dostęp API również w ofercie bezpłatnej. | 5 kanałów staqly za €12/mies. rocznie jest atrakcyjne dla jednej osoby. API/MCP nie wystarczy jako jedyny wyróżnik. |
| [Metricool](https://metricool.com/pricing/) | Starter €20/mies. miesięcznie lub €16 rocznie dla 5 marek; Advanced od €54 miesięcznie lub €43 rocznie. X jako dodatek; „unlimited” podlega fair use. Analityka, raporty i akceptacje. | Mocny konkurent w analityce; nie udawać równoważności całej funkcjonalności. Marka obejmuje kilka sieci, więc nie porównywać jej wprost z kanałem. |
| [SocialBee](https://socialbee.com/pricing/) | Bootstrap $29/5 profili, Accelerate $49/10, Pro $99/25 miesięcznie; Pro ma 5 workspace’ów i 3 użytkowników/workspace. Publikuje obietnicę unlimited AI content generation. | Wyraźne zastosowania planów, edycja i workflow mają większą wartość niż długa lista technicznych integracji. Ich AI nie dowodzi opłacalności nielimitowanego Higgsfield video. |
| [Postiz](https://postiz.com/pricing) | Standard $29/5 kanałów, Team $39/10, Pro $49/30, Ultimate $99/100 miesięcznie; od Team nielimitowani współpracownicy, API/CLI/MCP na wszystkich planach, osobne liczniki AI obrazów i wideo. | Najbliższy konkurent. Zachowujemy mocny pakiet kanałów, ale wyceniamy AI wspólnym budżetem, bo obrazy i filmy mają różne koszty. |
| [Ayrshare](https://www.ayrshare.com/pricing/) | Premium $149/1 profil, Launch $299/do 10, Business $599/30 miesięcznie. Profil to marka/klient z wieloma sieciami. Dodatki i większa skala osobno. | To również infrastruktura dla innych SaaS, nie bezpośredni odpowiednik ceny pojedynczego użytkownika. Nie jest obecnym kosztem staqly: kod publikuje przez własne integracje platform. |

Ograniczenie konkurencyjności: 3 osoby na staqly Plus to €87/mies., podczas gdy Postiz Team publikuje $39 z nielimitowanym zespołem. Zachowanie cen i modelu za miejsce nie pozwala uczciwie obiecać przewagi cenowej dla każdego zespołu. Wyróżniki: zintegrowane edytory, automatyzacja i otwarta wersja lokalna; nie „najtańsze dla agencji”.

## AI: GPT oraz Higgsfield

[Oficjalny cennik OpenAI](https://developers.openai.com/api/docs/pricing), standardowe krótkie konteksty, USD / 1 mln tokenów:

| Model | Wejście bez cache | Wyjście | Przykładowy koszt 2 000 wejścia + 500 wyjścia |
|---|---:|---:|---:|
| gpt-6-luna | $0.10 | $0.50 | $0.00045 |
| gpt-6.1-sol | $2 | $10 | $0.009 |
| gpt-6-astra | $10 | $50 | $0.045 |

To obliczenie dla jednego wywołania, nie całej rozmowy z narzędziami. Długie historie, obrazy, wielokrotne kroki, inne poziomy przetwarzania i regionalne dopłaty zmieniają koszt. Rekomendacja startowa: przetestować Luna do krótkich przeredagowań, Sol do asystenta z narzędziami; Astra nie powinna być domyślnym modelem przy tych cenach. Worktree nie zmienia skonfigurowanego modelu ani kluczy w `.env`.

[Higgsfield Cloud](https://cloud.higgsfield.ai/) pokazuje m.in. Soul Standard od $0.094/obraz. [Dokumentacja rozliczeń](https://docs.higgsfield.ai/docs/concepts/billing-and-retention.md) nakazuje sprawdzić uwierzytelniony `/estimate/<model>` z właściwymi parametrami; przykład w dokumentacji nie jest ofertą dla konkretnego konta. Nie utożsamiać subskrypcji aplikacji Higgsfield z kredytami API.

Kod staqly szacuje Soul na $0.10/obraz, Kling 2.5 Turbo na $0.07/sekundę. Druga liczba pochodzi z istniejącego katalogu/odpowiednika modelu na fal.ai, NIE została potwierdzona jako stawka Twojego konta Higgsfield. Przy tych założeniach $1 pozwala orientacyjnie na 10 obrazów Soul albo 2 klipy po 5 s i trochę tekstu. Nie obiecujemy tych ilości w pricingu. Przed produkcją trzeba sprawdzić aktualne endpointy, stawki i `AI_PRICES`; nowszych modeli w katalogu Higgsfield nie przedstawiamy jako dostępnych w staqly.

Nowy kod rezerwuje najdroższą z dostępnych ścieżek mediów; płatny fallback asystenta nie omija limitu. Rozmowy rezerwują koszt zależny od modelu i wielkości wejścia, z maksymalnie 2 048 tokenami wyjścia. Brak raportu zużycia lub przerwany stream nie jest traktowany jako darmowe wywołanie. Rezerwacje są szacunkowe, nie stanowią gwarancji wysokości faktury dostawcy.

## Jednostkowa opłacalność — jawny scenariusz, nie gwarancja zysku

Najtrudniejszy wariant: jeden użytkownik na workspace, roczna cena, pełne wykorzystanie AI, X i miejsca. Bez VAT jako przychodu. Założenia, dopóki nie ma Twoich faktur:

- €1 = $1 dla kalkulacji, bez korzyści z aktualnego kursu; osobno stres walutowy poniżej.
- X: $0.015/publikację bez linku, $0.200 z linkiem, $0.005/odczyt pojedynczego posta — [oficjalny cennik](https://docs.x.com/x-api/getting-started/pricing). Waga 14 zabezpiecza koszt linku.
- Płatności: 4% przychodu + €0.30 co miesiąc jako ostrożne ZAŁOŻENIE, nie oferta Stripe. Przy płatności rocznej stała część będzie zwykle pobierana raz, ale tutaj celowo liczymy ją miesięcznie.
- Przechowywanie: €0.05/GB/mies. jako budżet na storage i replikację, nie stawka konkretnego hostingu.
- Rezerwa operacyjna na workspace: €1 / €2 / €4 / €8 miesięcznie. To założenie na bazę, kolejki, pocztę, transfer i obsługę, a nie zmierzony limit kosztu.

| EUR / workspace / miesiąc | Starter | Plus | Pro | Business |
|---|---:|---:|---:|---:|
| Przychód przy płatności rocznej | 12.00 | 23.00 | 47.00 | 79.00 |
| AI | 1.00 | 2.00 | 5.00 | 10.00 |
| X publikacja + odczyty | 1.25 | 2.50 | 6.25 | 12.50 |
| Storage | 0.10 | 0.50 | 1.50 | 5.00 |
| Płatności | 0.78 | 1.22 | 2.18 | 3.46 |
| Rezerwa operacyjna | 1.00 | 2.00 | 4.00 | 8.00 |
| Razem modelowany koszt | 4.13 | 8.22 | 18.93 | 38.96 |
| Pozostała marża kontrybucyjna | 7.87 (65.6%) | 14.78 (64.3%) | 28.07 (59.7%) | 40.04 (50.7%) |

Nie jest to zysk netto: nie obejmuje pełnego kosztu pracy, CAC, podatku dochodowego, zwrotów, chargebacków, rabatów, stałych kosztów przy małej liczbie klientów ani nieograniczonego pobierania plików. Sam limit przechowywania nie ogranicza transferu. Przy wzroście kosztu dolarowego AI+X o 20% i podwojeniu rezerwy operacyjnej pozostaje odpowiednio €6.42 / €11.88 / €21.82 / €27.54, przed tymi kosztami.

Dodatkowe miejsca zwiększają przychód bez zwiększania wspólnej puli; ta tabela używa tylko jednego miejsca. Dodatki Stripe mogą podnosić limity — każdy wymaga osobnej kalkulacji. Istniejące pakiety X pozostają €10/200, €40/1 000, €150/4 000, płatne z góry; nie dotykano katalogu Stripe.

**Zysk z każdej rejestracji jest niemożliwy do zagwarantowania przy bezpłatnym trialu.** Trial Starter ma obecnie te same skończone pule co Starter, wymaga karty, a kod zawiera zabezpieczenia ponownych triali. AI+X publikacja i odczyty mogą kosztować do około $2.25 w jednym miesiącu UTC, a trial przechodzący przez 1. dzień miesiąca może skorzystać z dwóch pul (około $4.50), plus infrastruktura. To koszt pozyskania. Przy tej skrajnej kwocie i bazowej marży Starter €7.87 sam pierwszy płatny miesiąc pokrywa wydatki triali w kohorcie dopiero przy konwersji około 57.2% — bez pozostałych kosztów. Jeżeli to za dużo: osobny jednorazowy budżet trialu lub start płatny; nie ukrywać tego za obietnicą „profit z każdej rejestracji”.

## Wspólne limity platform

[LinkedIn Community Management Development](https://learn.microsoft.com/en-us/linkedin/marketing/increasing-access?view=li-lms-2026-10): dokumentacja podaje 500 wywołań aplikacji i 100 na członka na dobę. To wywołania API, nie posty. [YouTube](https://developers.google.com/youtube/v3/determine_quota_cost) podaje obecnie osobne domyślne 100 wywołań `videos.insert` na dobę i reset o północy Pacific; starsze założenie „1 600 jednostek/upload, 6 filmów/dzień” w repo jest nieaktualne.

Kod dodaje trwałe rezerwacje przed HTTP dla publikacji, odpowiedzi, repostów i statystyk: LinkedIn domyślnie 400 wywołań/dzień łącznie i 40/workspace; YouTube 90 rozpoczętych uploadów łącznie i 9/workspace. Błędy i ponowienia też liczą się. Liczniki działają w transakcji także przy wielu workerach. Zmiana strefy/czasu letniego YouTube jest uwzględniona. Zmiennymi `LINKEDIN_DAILY_API_CALLS` i `YOUTUBE_DAILY_UPLOADS` operator dopasowuje pulę do zatwierdzonego konta. To górny limit 10%, NIE zarezerwowane lub gwarantowane 10% dla każdej ze 100 firm: łączna pula może zostać wykorzystana wcześniej.

Ograniczenia: pozostałe operacje OAuth/łączenia kont mają pozostawiony zapas, ale nie są liczone tym wrapperem. Inne aplikacje korzystające z tego samego konta dostawcy też nie są tu widoczne. Limity per-member/per-endpoint platform nadal obowiązują. Własne aplikacje deweloperskie workspace’u są obecnie liczone konserwatywnie w tej samej puli; rozdzielenie po kluczu aplikacji to przyszła poprawa, nie obietnica pricingu.

Ochrona dotyczy workspace’u, nie tożsamości osoby posiadającej wiele workspace’ów. Liczniki dziedziczą istniejący cykl życia danych workspace’u: jego trwałe usunięcie usuwa też zużycie z agregatu. Przed udostępnieniem masowego tworzenia/usuwania workspace’ów potrzebny będzie niezależny rejestr użycia aplikacji dostawcy. Istniejące rozliczanie publikacji X może dopuścić niewielkie przekroczenie przy równoległych publikacjach; nie jest ścisłą rezerwacją kosztu przed HTTP. Dlatego te mechanizmy nie zastępują limitu wydatków na koncie dostawcy.

## Wdrożenie i obserwacja

1. Sprawdzić realne limity aplikacji LinkedIn/YouTube, model GPT, uwierzytelnione wyceny Higgsfield i budżety kont dostawców. Ustawić alerty i zatrzymanie wydatków tam, gdzie dostawca je obsługuje; nie włączać nieograniczonego auto-recharge.
2. Wdrożyć backend, dashboard, stronę i dokumentację razem. Ceny nie wymagają zmiany Stripe, lecz istniejące entitlements trzeba porównać z nowymi limitami.
3. Ocenić istniejących klientów przed publikacją: nowe wartości dotyczą także obecnych planów. Kod nie usuwa danych; workspace ponad limitem nie doda kolejnego zasobu. To zmniejszenie uprawnień wymaga komunikacji lub polityki zachowania starych limitów. Ten worktree nie wysyła wiadomości, nie zmienia subskrypcji i nie wykonuje migracji produkcji.
   Skończone limity kluczy w Pro i Business oznaczają też istniejące zachowanie procedury odzyskiwania: nowy klucz administratora zastępuje stare klucze administratora, które są unieważniane podczas odzyskiwania dostępu.
4. Mierzyć realny koszt/workspace: AI i X, transfer, maksymalne przechowywanie, odsetek odrzuceń, czas czekania na wspólną pulę i konwersję trial → paid. Skorygować rezerwę infrastruktury na podstawie faktur, nie deklarować zysku netto z powyższego modelu.

## Weryfikacja lokalna

- `pnpm check`: końcowy przebieg zakończony sukcesem, 40/40 zadań formatowania, lint, typów, testów i limitów rozmiaru. Obejmuje budowę strony i dokumentacji, kontrolę linków oraz 487 testów dashboardu.
- Cross-client: 126/126 testów REST, CLI, MCP, TypeScript i Python przeszło.
- PostgreSQL z `-race`: przeszły testy współbieżnej rezerwacji wspólnej puli, odnowienia limitów UTC/Pacific, limitu statystyk X i blokady płatnych odczytów po zawieszeniu planu. Osobno przeszła regresja rezerwowania droższego fallbacku AI.
- Docker: pierwszy pełny przebieg miał 199 testów pozytywnych, jeden błąd i siedem testów pominiętych wskutek tego błędu. Test odzyskiwania zakładał dawny nielimitowany Pro i dalszą ważność starego klucza administratora. Poprawiono oczekiwanie oraz użycie nowego klucza w kolejnych testach. Ponowiony pełny scenariusz cloud (pliki 07–10): 94/94 przeszły, bez pominiętych testów.
- UI: kontrola axe ekranów cloud (w tym Plan & usage) przeszła. Pełny przebieg przerwano po trzech rozbieżnościach testów edytora z aktualnym polem tytułu: 10 testów przeszło, jeden przerwano, 77 nie uruchomiono. Osobny test onboardingu oczekuje nieistniejącego przycisku „Continue” w kroku agenta, który ma „Make a key for Claude Code” i „Not now”; zatrzymuje się przed krokiem planu. Te kroki i testy nie były zmieniane w ramach cennika. Nie deklarujemy pełnego zielonego UI.
- Ręczna ocena wyglądu cennika na różnych szerokościach nie została wykonana: narzędzie podglądu zwracało `The browser access host is unavailable`. Statyczny build i sprawdzanie linków przeszły.

Testy używały lokalnych atrap AI i platform, bez płatnych wywołań GPT/Higgsfield. Nie wdrażano zmian ani nie zmieniano produktów/subskrypcji Stripe.
