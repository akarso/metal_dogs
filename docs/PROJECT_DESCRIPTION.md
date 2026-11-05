# Metal Dogs - project description

## General gist of it
This will be shop with t-shirt merch, integrated with print on demand service. Store backend will be shopanda (https://github.com/akarso/shopanda) in headless mode, frontend will be mainly t-shirt creator with AI additions, communicates with backend on rest endpoints

# Założenia architektoniczne projektu

> Dokument koncepcyjny będący podstawą do opracowania roadmapy. Opisuje docelowy produkt, główne decyzje architektoniczne i proponowany stos technologiczny. Nie jest jeszcze specyfikacją implementacyjną ani harmonogramem.

## 1. Cel projektu

Zbudować sklep z personalizowaną odzieżą dla właścicieli psów, którzy lubią metalową estetykę. Klient wybiera predefiniowany projekt graficzny, podaje imię psa, przesyła zdjęcie lub robi je bezpośrednio telefonem, a system generuje ilustrację i pokazuje podgląd na czarnej koszulce.

Produkt ma być prosty w obsłudze i ograniczać liczbę decyzji użytkownika. To nie ma być uniwersalny edytor graficzny ani „prompt playground”.

### Założenia produktowe na początek

- Odzież w kolorze czarnym.
- Grafiki monochromatyczne, czarno-białe.
- Nadruk z przodu produktu.
- Początkowo koszulki; bluzy mogą zostać dodane po zweryfikowaniu procesu.
- Predefiniowane projekty graficzne, bez dowolnego tworzenia stylu przez klienta.
- Początkowy katalog: około 30 szablonów; architektura powinna umożliwiać rozbudowę do 100–300 bez zmian w kodzie dla każdego szablonu.
- Klient wybiera gatunek muzyczny, następnie styl/konkretny projekt, podaje teksty, dodaje zdjęcie i generuje podgląd.
- Podstawowym edytowalnym tekstem jest imię psa. Dolny tekst lub inne elementy mogą być stałą częścią projektu; edycja dodatkowych tekstów może być opcjonalną funkcją.
- Klient ogląda osobno grafikę i mockup grafiki na koszulce, a następnie wybiera rozmiar i przechodzi do koszyka.
- Etsy może działać jako dodatkowy kanał sprzedaży, ale konfigurator i docelowy proces zamówienia są rozwijane niezależnie, z własnym storefrontem i Shopandą.

## 2. Zasady projektowe

1. **Ograniczona swoboda, przewidywalny rezultat.** Klient wybiera spośród przygotowanych projektów, zamiast wpisywać dowolne prompty.
2. **AI odpowiada za ilustrację i integrację wizualną.** Model może generować portret, ozdobniki i stylizowaną typografię na podstawie wzorca. Nie zakładamy, że cały tekst da się niezawodnie wyrenderować klasycznym fontem.
3. **Wynik produkcyjny jest oddzielony od mockupu.** Podgląd nie jest plikiem wysyłanym do drukarni.
4. **Każde zamówienie wiąże się z niezmienną wersją zatwierdzonego projektu.** Późniejsza zmiana szablonu nie może zmienić wcześniej zaakceptowanego zamówienia.
5. **Dostawca AI i dostawca POD nie powinni być mocno związani z logiką domenową.** Integracje należy ukryć za małymi interfejsami/adaptorami.
6. **Najpierw walidacja jakości, potem automatyzacja.** Przed budową pełnego sklepu należy przetestować generowanie na rzeczywistych zdjęciach i ustalonych szablonach.
7. **Bez niepotrzebnych frameworków i mikroserwisów.** System powinien być możliwie mały, dopóki skala nie uzasadni wydzielenia kolejnych komponentów.

## 3. Główne ścieżki użytkownika

### 3.1 Standardowa ścieżka

1. Wybór gatunku muzycznego.
2. Wybór stylu lub projektu z galerii.
3. Podanie imienia psa i ewentualnie innych dostępnych tekstów.
4. Zapoznanie się z instrukcją dotyczącą odpowiedniego zdjęcia dla danego szablonu.
5. Dodanie zdjęcia z urządzenia albo wykonanie zdjęcia aparatem telefonu.
6. Walidacja pliku i wysłanie zdjęcia.
7. Wygenerowanie grafiki.
8. Prezentacja podglądu samej grafiki.
9. Prezentacja mockupu grafiki na czarnej koszulce.
10. Opcjonalne ponowne wygenerowanie w ramach limitu prób.
11. Wybór rozmiaru i dodanie do koszyka.
12. Checkout i zamówienie.

### 3.2 Zdjęcie bezpośrednio z telefonu

Na urządzeniach mobilnych interfejs powinien umożliwiać:
- wybór istniejącego zdjęcia z galerii;
- uruchomienie aparatu i zrobienie zdjęcia na miejscu, jeśli przeglądarka i urządzenie to obsługują.

Preferowana implementacja pierwszej wersji: standardowy element HTML `<input type="file" accept="image/*" capture="environment">` lub równoważny mechanizm progresywny. Atrybut `capture` jest wskazówką dla przeglądarki, a nie gwarancją identycznego zachowania na wszystkich urządzeniach. Zawsze należy pozostawić możliwość wybrania pliku z galerii. Osobny widok aparatu oparty na `getUserMedia()` nie jest wymagany na MVP.

Instrukcje fotografowania powinny być zależne od szablonu, np.:
- głowa z przodu;
- głowa pod kątem;
- cała sylwetka z profilu;
- proste, mało rozpraszające tło;
- ostre zdjęcie i odpowiednie oświetlenie;
- pies w całości widoczny, jeśli projekt tego wymaga.

Przed wysłaniem należy pokazać wybrane zdjęcie i umożliwić jego zmianę. Nie należy obiecywać, że automatyczna walidacja jakości zdjęcia będzie bezbłędna.

## 4. Architektura logiczna

Na początek preferowana jest modularna aplikacja backendowa, a nie zestaw niezależnych mikroserwisów.

```text
Storefront (HTML/CSS/vanilla JS)
    |
    | REST API
    v
Shopanda / Go backend
    |
    +-- Catalog / Product variants
    +-- Cart / Checkout / Orders
    +-- Customizer domain module
    |      +-- Template catalog
    |      +-- Upload management
    |      +-- Generation jobs
    |      +-- Design versions / approval
    |      +-- Preview composition
    |      +-- Production artwork
    |
    +-- Job queue / worker
    |      +-- Image preparation
    |      +-- AI provider adapter
    |      +-- Output validation
    |      +-- Mockup / production rendering
    |
    +-- PostgreSQL
    +-- S3-compatible object storage
    +-- External image-generation API
    +-- POD / fulfillment provider
```

Powyższy diagram opisuje granice odpowiedzialności. Nie przesądza, czy każdy element będzie osobnym pakietem, procesem czy modułem Shopandy. Przed implementacją trzeba zweryfikować obecne możliwości i ograniczenia Shopandy oraz jej API.

### 4.1 Shopanda

Shopanda pozostaje głównym systemem e-commerce:
- katalog produktów i wariantów;
- ceny;
- koszyk i checkout;
- zamówienia;
- dane klienta;
- płatności i integracje e-commerce, zgodnie z faktycznie obsługiwanymi możliwościami.

Konfigurator nie powinien implementować ponownie mechanizmów sklepowych, które już istnieją w Shopandzie. Należy sprawdzić faktyczny stan API i obsługę guest checkout przed ustaleniem kontraktów.

### 4.2 Moduł konfiguratora

Odpowiada za:
- definicje szablonów;
- reguły wyboru i walidacji zdjęć;
- teksty oraz parametry konkretnej konfiguracji;
- uruchamianie i śledzenie generowania;
- wyniki generowania i ich wersjonowanie;
- akceptację projektu przez klienta;
- przygotowanie pliku produkcyjnego;
- powiązanie zatwierdzonej grafiki z pozycją zamówienia.

### 4.3 Przetwarzanie asynchroniczne

Generowanie obrazu może trwać dłużej niż typowe żądanie HTTP. API powinno utworzyć zadanie i zwrócić jego identyfikator. Frontend może okresowo sprawdzać status; później można rozważyć SSE lub WebSocket, jeśli będzie to uzasadnione.

Przykładowe statusy:
- `draft`
- `upload_received`
- `queued`
- `generating`
- `validating`
- `preview_ready`
- `approved`
- `failed`
- `ordered`
- `production_ready`

Nazwy są propozycją i powinny zostać dopracowane w roadmapie.

## 5. Proponowany stos technologiczny

### Frontend

- HTML, CSS i vanilla JavaScript.
- Vite tylko jako narzędzie budowania, jeśli okaże się potrzebne.
- REST API do komunikacji z backendem.
- Na MVP bez Reacta, Vue i Angulara.
- Podgląd można realizować przez HTML/CSS i warstwy obrazu, a w razie potrzeby przez Canvas API.
- Fabric.js lub Konva.js tylko wtedy, gdy pojawi się realna potrzeba interaktywnego manipulowania elementami. Na obecnym etapie klient nie przesuwa ani nie skaluje obiektów.
- Mobilny upload z obsługą wyboru pliku i opcjonalnego uruchomienia aparatu przez `capture`; brak konieczności implementacji własnego interfejsu aparatu.

### Backend

- Go, w ramach istniejącej architektury Shopandy, jeśli jej obecne granice modułów na to pozwalają.
- REST API.
- PostgreSQL dla danych domenowych i metadanych.
- S3-compatible object storage dla zdjęć, wygenerowanych ilustracji, podglądów i plików produkcyjnych.
- Worker/kolejka do zadań generowania i przetwarzania obrazów. Preferować istniejące mechanizmy Shopandy, jeśli zapewniają potrzebne retry, obserwowalność i kontrolę współbieżności.
- Nie wprowadzać Pythona wyłącznie do wywoływania zewnętrznego API obrazowego.

### AI i integracje obrazowe

- Dostawca obrazu powinien być wymienialny poprzez mały interfejs/adaptor w Go.
- Wysyłanie zdjęcia, wzorca referencyjnego, tekstów i instrukcji odbywa się po stronie serwera.
- Klucze API nigdy nie są udostępniane w frontendzie.
- Nie zakładać, że możliwości dostępne w interfejsie konsumenckim danego produktu są identyczne z możliwościami jego API.
- Na początku wybrać jednego dostawcę i ocenić jakość, koszt, czas odpowiedzi, zasady przetwarzania danych oraz możliwość komercyjnego wykorzystania wyników.
- Oddzielić przygotowanie wejścia, wywołanie modelu, walidację wyniku i składanie mockupu.
- Tekst może być generowany przez model jako część kompozycji, ponieważ niestandardowa typografia jest częścią stylu. Należy jednak sprawdzać pisownię, zwłaszcza imienia psa. Jeśli dany projekt wymaga absolutnej zgodności tekstu, rozważyć osobną warstwę tekstową lub procedurę korekty.
- Ponowne generowanie powinno być ograniczone i kontrolowane kosztowo.

### Renderowanie grafiki

- Mockup produktu jest niezależny od pliku produkcyjnego.
- Preferować przygotowane mockupy czarnych koszulek z grafiką nakładaną w wyznaczonym obszarze.
- Finalny plik produkcyjny musi spełniać wymagania konkretnego dostawcy i technologii druku.
- Nie zakładać, że screenshot podglądu przeglądarkowego jest plikiem do druku.
- Wymagania dotyczące rozdzielczości, wymiarów, przezroczystości i profilu koloru należy ustalić z dostawcą POD.
- Jeśli model nie zwraca prawidłowej przezroczystości, przewidzieć etap usuwania tła lub przetwarzania maski.
- Przechowywać wersję zatwierdzoną i powiązać ją z zamówieniem.

## 6. Model danych — pojęcia domenowe

Poniższe pojęcia należy uwzględnić w projekcie schematu; nie przesądzają jeszcze o konkretnych tabelach.

### Template
- identyfikator i status publikacji;
- gatunek muzyczny i kategoria stylistyczna;
- wzorzec referencyjny;
- instrukcje generowania i parametry modelu;
- wymagania dotyczące zdjęcia;
- definicje edytowalnych tekstów;
- domyślne teksty nieedytowalne;
- konfiguracja wyjścia i obszaru kompozycji;
- wersja szablonu.

### Design / Configuration
- identyfikator projektu;
- identyfikator szablonu i jego wersja;
- wybrane wartości tekstów;
- identyfikator przesłanego zdjęcia;
- status;
- wynik generowania;
- podgląd;
- dane potrzebne do odtworzenia konfiguracji.

### Asset
- typ zasobu: źródło, wynik AI, mockup, plik produkcyjny;
- lokalizacja w object storage;
- typ MIME, rozmiar i wymiary;
- hash kontrolny;
- data utworzenia i data planowanego usunięcia;
- relacja z projektem/zamówieniem.

### Generation job
- identyfikator projektu;
- dostawca i model;
- wersja prompta/szablonu;
- status, liczba prób i czasy wykonania;
- koszt, jeśli dostawca udostępnia dane;
- kody błędów i informacje do diagnostyki bez niepotrzebnego przechowywania danych osobowych.

### Order design snapshot
Po zatwierdzeniu projektu należy utrwalić dokładną wersję grafiki, konfigurację i identyfikatory plików. Zmiana szablonu lub ponowne generowanie nie może zmienić projektu już zamówionego.

## 7. Przykładowe granice REST API

To wstępne propozycje ścieżek, nie istniejące endpointy Shopandy:

```http
GET  /api/customizer/templates
GET  /api/customizer/templates/{id}

POST /api/customizer/uploads
POST /api/customizer/designs
GET  /api/customizer/designs/{id}

POST /api/customizer/designs/{id}/generate
GET  /api/customizer/generation-jobs/{id}

POST /api/customizer/designs/{id}/approve
POST /api/cart/items
```

Wymagania:
- upload z limitem rozmiaru i typów plików;
- autoryzacja lub bezpieczne, trudne do odgadnięcia tokeny dla projektów gościa;
- idempotencja dla operacji, które mogą zostać ponowione;
- kontrola dostępu do zasobów;
- walidacja po stronie serwera niezależnie od walidacji frontendowej;
- wersjonowanie konfiguracji;
- brak kluczy dostawcy AI w odpowiedziach API.

Przed implementacją należy dostosować te ścieżki do konwencji REST i istniejących możliwości Shopandy.

## 8. Wymagania jakościowe i bezpieczeństwo

### Zdjęcia i prywatność
- Informować klienta, że zdjęcie zostanie przetworzone przez zewnętrznego dostawcę AI.
- Ustalić politykę retencji źródeł i wygenerowanych plików.
- Ograniczyć dostęp do zdjęć i stosować krótkotrwałe, podpisane adresy URL, jeśli storage jest prywatny.
- Nie umieszczać zdjęć ani danych osobowych w publicznych adresach.
- Obsłużyć usuwanie danych zgodnie z przyjętą polityką i obowiązującymi wymaganiami prawnymi.
- Usuwać metadane EXIF, jeśli nie są potrzebne.
- Walidować MIME, rzeczywisty format, rozmiar i wymiary plików; nie ufać wyłącznie rozszerzeniu.

### Koszty i niezawodność
- Limit liczby generowań na projekt i ochrona przed automatycznym nadużyciem.
- Retry tylko dla błędów przejściowych, z ograniczeniem liczby prób.
- Obsługa timeoutów, limitów dostawcy i błędów API.
- Rejestrowanie kosztu i czasu generowania, jeśli dane są dostępne.
- Nie wykonywać kosztownego generowania wielokrotnie przy ponowieniu tego samego żądania bez świadomej decyzji.
- Statusy błędów powinny umożliwiać użytkownikowi sensowne wznowienie procesu.

### Jakość produkcyjna
- Wyraźnie odróżnić podgląd od zatwierdzonego projektu.
- Ustalić minimalną jakość grafiki i wymagania dla każdego produktu.
- Przechowywać finalny plik oraz jego hash.
- Zweryfikować rzeczywiste próbki nadruku przed uruchomieniem sprzedaży.

## 9. Poza zakresem pierwszej wersji

- Dowolne prompty wpisywane przez klienta.
- Edytor z przeciąganiem, obracaniem i skalowaniem elementów.
- Pełny system własnych fontów i automatycznego dopasowywania tekstu do krzywych.
- Własna infrastruktura treningu lub hostowania modeli AI.
- Własna drukarka i produkcja odzieży na miejscu.
- Wiele kolorów odzieży i wielokolorowe grafiki.
- Rozbudowany system kont użytkowników, historii projektów i galerii publicznej, o ile nie będzie konieczny do sprzedaży.
- Osobny mikroserwis w Pythonie bez konkretnego uzasadnienia.
- Pełna automatyzacja wszystkich integracji POD przed walidacją jakości i popytu.

## 10. Założenia do zweryfikowania przed roadmapą

1. Który model/API najwierniej zachowuje psa, wzorzec i dokładny tekst.
2. Jaki jest koszt jednej zaakceptowanej grafiki, uwzględniając nieudane próby.
3. Czy model dostarcza użyteczne wyjście do druku, czy potrzebne jest dodatkowe usuwanie tła i obróbka.
4. Jakie są wymagania techniczne wybranego dostawcy POD.
5. Jakie mechanizmy jobów, storage i API faktycznie oferuje aktualna wersja Shopandy.
6. Jak zachowuje się mobilne fotografowanie w docelowych przeglądarkach iOS/Android.
7. Czy typografia generowana przez AI jest wystarczająco poprawna dla imion o różnej długości i z polskimi znakami.
8. Jaka polityka retencji zdjęć i danych będzie właściwa.
9. Czy Etsy będzie korzystać z tego samego pipeline'u generowania, czy początkowo będzie osobnym kanałem sprzedaży z ręcznym przenoszeniem zamówień.

## 11. Kryteria gotowości do opracowania roadmapy

Przed rozpisaniem pełnej roadmapy powinny być znane:
- wynik testów AI na reprezentatywnych zdjęciach i szablonach;
- wybrany dostawca POD i wymagania plików;
- potwierdzone możliwości aktualnego API Shopandy;
- docelowy sposób obsługi generowania asynchronicznego;
- wstępna polityka przechowywania i usuwania zdjęć;
- minimalny zakres MVP.

Po potwierdzeniu tych punktów można rozbić projekt na etapy: proof of concept generowania, konfigurator, integracja ze sklepem, checkout, fulfillment i optymalizacja.

