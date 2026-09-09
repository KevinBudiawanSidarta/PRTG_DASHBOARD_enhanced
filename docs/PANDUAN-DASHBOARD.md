# Panduan Menggunakan BIA Platform Dashboard

Dokumen ini menjelaskan cara menjalankan dan menggunakan setiap fitur di **BIA Platform Dashboard** dengan bahasa yang mudah dipahami — cocok untuk siapa saja, termasuk yang belum pernah pakai dashboard ini sebelumnya.

## Daftar Isi

1. [Apa itu dashboard ini?](#1-apa-itu-dashboard-ini)
2. [Cara mengaksesnya](#2-cara-mengaksesnya)
3. [Mengenal tampilan utama](#3-mengenal-tampilan-utama)
4. [Live Monitoring](#4-live-monitoring)
5. [AI Impact Analysis](#5-ai-impact-analysis)
6. [Knowledge Base (CRUD)](#6-knowledge-base-crud)
7. [Executive Overview](#7-executive-overview)
8. [Business Impact Analysis (BIA)](#8-business-impact-analysis-bia)
9. [Business Impact](#9-business-impact)
10. [Service Mapping](#10-service-mapping)
11. [Financial Profiles](#11-financial-profiles)
12. [Detail Insiden (panel samping)](#12-detail-insiden-panel-samping)
13. [Tips & Pertanyaan Umum](#13-tips--pertanyaan-umum)

---

## 1. Apa itu dashboard ini?

BIA Platform adalah dashboard untuk memantau **kesehatan infrastruktur IT** (lewat sensor PRTG) dan menerjemahkannya menjadi **dampak bisnis dan kerugian finansial** yang mudah dipahami oleh manajemen — bukan cuma "server down", tapi "server ini down = potensi rugi Rp X per jam, mempengaruhi Y karyawan/pelanggan".

Singkatnya, dashboard ini menjawab tiga pertanyaan:
- **Apa yang sedang bermasalah?** (Live Monitoring)
- **Berapa kerugiannya kalau ini terus down?** (AI Impact Analysis, Business Impact)
- **Layanan bisnis mana yang paling kritis dan butuh perhatian duluan?** (Executive Overview, BIA, Incident Priority)

## 2. Cara mengaksesnya

Setelah dashboard dijalankan (lihat panduan menjalankan di percakapan sebelumnya, atau minta tim teknis menjalankan `services/api` + `apps/web`), buka browser dan kunjungi:

```
http://localhost:3000
```

Kalau muncul pesan **"⚠️ API connection issue"** di bagian atas halaman, artinya bagian backend (API) belum jalan atau bermasalah — hubungi tim teknis.

## 3. Mengenal tampilan utama

Dashboard punya dua bagian:

- **Sidebar kiri** — daftar menu/tab. Angka merah di sebelah nama menu adalah **badge peringatan** (misalnya jumlah sensor down atau insiden yang masih terbuka).
- **Area utama (kanan)** — isi dari tab yang sedang dipilih.

Di pojok kiri bawah ada indikator **"Live · 15s refresh"** — artinya data di layar otomatis diperbarui setiap 15 detik tanpa perlu refresh manual.

---

## 4. Live Monitoring

**Untuk apa:** Melihat status semua sensor PRTG (perangkat/server/jaringan yang dipantau) secara real-time.

**Yang bisa kamu lakukan:**
- **4 kartu ringkasan** di atas: Total Sensor, yang **Operational (UP)**, yang **Down**, dan yang **Unknown**. Klik salah satu kartu untuk otomatis memfilter grid sensor di bawahnya.
- **Chip filter** (🌐 Semua, ✅ Aktif, 🔴 Down, ⚪ Unknown, ⚠️ Warning) — cara cepat menyaring sensor berdasarkan status.
- **Kolom pencarian** — ketik nama device atau sensor untuk mencarinya langsung.
- **Grid sensor** — setiap kartu menunjukkan 1 sensor: nama device, nama sensor, ID PRTG, dan status (warna merah = down, hijau = up).
- **Pagination** di bawah grid — karena sensor bisa jumlahnya ratusan, ditampilkan 20 per halaman.
- **Event Feed** di sisi kanan — log perubahan status sensor terbaru (misalnya "sensor X berubah dari UP ke DOWN, 2 menit lalu").
- Tombol **⟳ Refresh** untuk memuat ulang data secara manual.

> 💡 **Tips:** Ini adalah tab paling teknis — cocok untuk tim IT/NOC yang perlu tahu detail sensor mana yang bermasalah.

---

## 5. AI Impact Analysis

**Untuk apa:** Menerjemahkan sensor yang sedang down menjadi estimasi kerugian bisnis secara otomatis, menggunakan aturan dari **Knowledge Base**.

**Yang bisa kamu lakukan:**
- **Scenario Simulator** — masukkan durasi downtime (dalam jam) untuk melihat proyeksi kerugian kalau masalah ini berlangsung sekian lama.
- **Analisis Sensor Spesifik** — pilih satu sensor tertentu dari dropdown, lalu klik **🤖 Analyze** untuk melihat analisis khusus sensor itu saja (berguna untuk simulasi "bagaimana jika sensor ini down", walaupun statusnya sedang normal).
- Tombol **🔄 Re-analyze** (muncul kalau ada sensor down) — menjalankan ulang analisis untuk **semua** sensor yang sedang down.
- **Kartu hasil analisis** — tiap kartu mewakili satu sensor bermasalah, menampilkan:
  - Tingkat risiko (LOW / MEDIUM / HIGH / CRITICAL)
  - Kerugian per jam & proyeksi sesuai durasi yang kamu masukkan di Scenario Simulator
  - SLA penalty dan estimasi waktu recovery
  - Klik **"▼ Show Details & Recovery"** untuk melihat rekomendasi AI, aturan Knowledge Base yang cocok, proses bisnis yang terdampak, dan **prosedur recovery** langkah demi langkah.

> 💡 **Tips:** Kalau sebuah sensor down tapi tidak ada "Matched Knowledge Base Rules", artinya belum ada aturan untuk device tersebut — tambahkan di tab **Knowledge Base** supaya analisisnya lebih akurat.

---

## 6. Knowledge Base (CRUD)

**Untuk apa:** Di sinilah kamu **mendefinisikan aturan dampak bisnis** per device/sensor — ini "otak" dari AI Impact Analysis. ("CRUD" artinya kamu bisa Create/buat, Read/lihat, Update/ubah, dan Delete/hapus aturan di sini.)

**Yang bisa kamu lakukan:**
- Lihat ringkasan: total entry, estimasi kerugian maksimum per jam, jumlah kategori, dan jumlah aturan prioritas tertinggi (P1).
- Filter cepat lewat **chip kategori** (Network, Server, Application, Database, Storage, Security), filter prioritas, dan kolom pencarian.
- Klik **"+ New Entry"** untuk membuat aturan baru. Field pentingnya:
  - **Device Pattern** *(wajib)* — potongan nama device di PRTG yang ingin dicocokkan (misal `Core-Switch-01`).
  - **Sensor Pattern** *(opsional)* — kosongkan kalau mau berlaku untuk semua sensor di device itu.
  - **Kategori & Priority** — untuk pengelompokan (P1 = paling kritis).
  - **Kerugian/Jam, SLA Penalty/Jam, Affected Users, Recovery Time** — angka-angka yang dipakai untuk kalkulasi otomatis.
  - **Deskripsi Dampak Bisnis** *(wajib)* — penjelasan singkat apa yang terjadi kalau device ini down.
  - **Proses Bisnis Terdampak** dan **Recovery Procedure** — opsional, tapi sangat membantu tim yang menangani insiden nanti.
- Klik salah satu baris entry untuk **expand** dan melihat detail lengkap, lalu tombol **✎ Edit** atau **🗑 Delete** akan muncul.

> 💡 **Tips:** Semakin lengkap Knowledge Base, semakin akurat hasil di tab AI Impact Analysis dan Business Impact.

---

## 7. Executive Overview

**Untuk apa:** Ringkasan tingkat tinggi untuk manajemen — insiden apa saja yang aktif dan berapa dampak finansialnya, tanpa perlu paham detail teknis.

**Yang bisa kamu lakukan:**
- **4 KPI card**: Open Incidents, Critical, 30-Day Impact (total kerugian dari insiden yang sudah selesai dalam 30 hari terakhir), dan Events 24h.
- **Tabel Active & Recent Incidents** — klik baris mana saja untuk membuka [panel detail insiden](#12-detail-insiden-panel-samping) lengkap dengan breakdown finansialnya.
- **Technical Events** di sisi kanan — log telemetri mentah dari PRTG.
- Kotak **"📌 Cara Baca Dashboard"** di bagian bawah menjelaskan bagaimana kalkulasi finansial bekerja.

---

## 8. Business Impact Analysis (BIA)

**Untuk apa:** Ini tab paling lengkap — analisis dampak bisnis dari berbagai sudut pandang, terbagi jadi 6 sub-bagian (tab kecil di bagian atas):

| Sub-tab | Isinya |
|---|---|
| **📊 Executive Summary** | KPI besar: berapa service yang normal, berapa yang kritis terdampak, estimasi user terdampak, kepatuhan SLA bulan ini, dan total eksposur finansial. |
| **🏢 Service Impact** | Tabel per business service: status (normal/degraded/down), ketersediaan (%), jumlah sensor bermasalah, dampak ke user, dan target SLA-nya. |
| **📋 Impact Matrix** | Daftar skenario "kalau kondisi teknis X terjadi, dampaknya Y" — bisa kamu tambah/edit/hapus sendiri lewat tombol **"+ Tambah Entry"**. Berguna sebagai referensi cepat tim ops. |
| **🎯 Incident Priority** | Urutan insiden mana yang harus ditangani duluan, dihitung dari skor gabungan Criticality × User Impact × Financial × Urgency. Semakin tinggi skor, semakin prioritas (P1 = paling mendesak). |
| **💰 Financial Impact** | Rincian kerugian finansial bulan berjalan per service — dipecah jadi revenue loss, business value loss, productivity loss, operational cost, dan SLA penalty. |
| **📈 SLA & Downtime** | Perbandingan target SLA vs aktual, sisa "jatah" downtime yang masih diperbolehkan, MTTR (rata-rata waktu perbaikan), dan MTBF (rata-rata waktu antar gangguan). |

> 💡 **Tips:** Kalau kamu manajer atau pengambil keputusan dan cuma punya waktu 1 menit, cukup buka **Executive Summary** — semua angka penting sudah dirangkum di sana.

---

## 9. Business Impact

**Untuk apa:** Fokus khusus ke **kerugian finansial** dari insiden — baik yang sudah selesai (resolved) maupun yang masih berlangsung (open).

**Yang bisa kamu lakukan:**
- 3 KPI: Total Impact dari insiden yang sudah selesai, jumlah insiden resolved, dan jumlah insiden yang masih open.
- **Impact Breakdown per Incident** — tabel insiden yang sudah selesai, diurutkan dari kerugian terbesar, lengkap dengan bar persentase relatif terhadap insiden terbesar. Klik baris untuk lihat detail.
- **Active Incidents — Impact Accumulating** — insiden yang masih berjalan, di mana kerugiannya terus bertambah selama belum ditutup.

---

## 10. Service Mapping

**Untuk apa:** Menghubungkan sensor teknis (PRTG) ke **business service** yang sebenarnya — supaya sistem tahu "kalau sensor ini down, service bisnis mana yang terdampak".

**Yang bisa kamu lakukan (dua panel berdampingan):**
- **Panel kiri — kelola Business Service:**
  - Pilih service dari dropdown, atau klik **"+ New Service"** untuk membuat baru.
  - Isi **nama service** dan **criticality** (P1 = paling kritis, P4 = paling rendah).
  - Tombol **✎ Edit** dan **🗑 Delete** untuk mengubah/menghapus.
- **Panel kanan — kelola pemetaan sensor:**
  - Lihat sensor apa saja yang sudah terhubung ke service yang dipilih, beserta **dependency weight**-nya (angka 0.01–1, seberapa besar pengaruh sensor itu terhadap service).
  - Tambahkan sensor baru lewat dropdown **"Tambah sensor..."**, atur bobotnya, lalu klik **Add**.
  - Klik **💾 Save Mapping** untuk menyimpan semua perubahan sekaligus.

> 💡 **Tips:** Tanpa mapping ini, insiden akan muncul sebagai "Unmapped Service" di tab lain — jadi ini langkah penting supaya semua data dampak bisnis akurat.

---

## 11. Financial Profiles

**Untuk apa:** Mengatur **asumsi keuangan** yang dipakai untuk menghitung kerugian — misalnya berapa pendapatan per jam suatu service, berapa transaksi per jam, dsb.

**Yang bisa kamu lakukan:**
- Lihat tabel semua financial profile yang aktif: service terkait, hourly revenue, dependency, loss probability, dan status (ACTIVE/VERSIONED).
- Klik **"+ New Profile"** untuk membuat profil baru (bisa untuk service tertentu, atau "Organization Default" kalau berlaku umum).
- Field yang diisi: Hourly Revenue, Transactions/Hour, Avg Transaction Value, Service Dependency (0–1), Loss Probability (0–1), Operational Cost/Hour, Penalty Fixed, Recovery Fixed.
- Klik **✎ Edit** pada profil yang aktif untuk membuat **versi baru** — sistem tidak menimpa data lama, tapi membuat versi baru dan menutup interval sebelumnya. Ini penting supaya insiden lama tetap dihitung dengan asumsi yang berlaku saat itu (auditable).
- Tombol **Deactivate** untuk menonaktifkan profil (bukan menghapus permanen).

> ⚠️ **Penting:** Karena sistemnya "versioned", mengedit profil **tidak mengubah riwayat lama** — jadi aman dipakai untuk laporan keuangan/audit.

---

## 12. Detail Insiden (panel samping)

Dari tab **Executive Overview**, **Business Impact**, atau dengan klik baris insiden di mana saja, akan muncul panel di sisi kanan layar berisi:

- Status & severity insiden, sensor & device terkait, dan durasi berjalan.
- **Estimated Financial Loss** — total kerugian, dipecah per kategori (revenue loss, recovery cost, operational cost, penalty exposure).
- **Input Snapshot** — angka-angka mentah yang dipakai untuk kalkulasi (untuk keperluan audit/transparansi).
- Kalau insiden masih **OPEN**, ada tombol **✓ Acknowledge** untuk menandai sudah ditangani.
- Kalau sudah **ACKNOWLEDGED**, ada tombol **✔ Close Incident** untuk menutup insiden.

> Catatan: kalkulasi finansial baru muncul **setelah insiden RESOLVED** — selama masih open, kamu akan melihat pesan "Financial calculation pending".

---

## 13. Tips & Pertanyaan Umum

**Q: Kenapa ada banyak sensor berstatus "unknown"?**
A: Biasanya karena sensor tersebut belum pernah di-polling atau statusnya belum diketahui oleh sistem monitoring. Tidak selalu berarti masalah.

**Q: Data tidak berubah walau saya klik tombol berkali-kali, apa itu normal?**
A: Kalau kondisi sensor memang belum berubah (masih down/up yang sama), hasil analisis wajar terlihat sama — bukan berarti tombolnya tidak bekerja.

**Q: Kenapa saya lihat pesan "⚠️ API connection issue"?**
A: Backend (API) sedang tidak bisa dihubungi. Cek apakah service API sudah dijalankan, atau hubungi tim teknis.

**Q: Apa bedanya "Business Impact Analysis (BIA)" dan "Business Impact"?**
A: **BIA** adalah analisis lengkap 6 sudut pandang (executive summary, service impact, matrix, priority, financial, SLA). **Business Impact** lebih sempit — fokus ke breakdown kerugian finansial per insiden saja.

**Q: Siapa yang perlu diberi tahu kalau muncul insiden CRITICAL?**
A: Lihat tab **Incident Priority** di BIA — insiden dengan skor tertinggi (P1) adalah yang paling mendesak untuk segera ditangani/dieskalasi.

---

*Dokumen ini dibuat untuk membantu pengguna baru memahami BIA Platform Dashboard. Untuk dokumentasi teknis (API endpoints, arsitektur), lihat [`docs/API.md`](API.md) dan [`README.md`](../README.md).*
