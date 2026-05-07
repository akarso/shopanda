# ADMIN_IMPROVEMENTS.md

## 🎯 Goal

Design an admin panel that:
- supports multi-store, multi-language, multi-currency
- remains intuitive and scalable
- avoids Magento-style over-engineering
- is structured around **user intent**, not database structure

---

## 🧠 Core Principles

### 1. Organize by Domain, Not Data
Navigation should reflect how users think:

❌ Wrong:
- Products
- Media
- Settings

✅ Right:
- Sales
- Catalog
- Customers
- Marketing
- Content
- Operations

---

### 2. Context is a First-Class Concept

Introduce a **global context switcher**:

[ Store ▼ ] [ Language ▼ ] [ Currency ▼ ]


This controls:
- product data
- content
- pricing
- configuration

👉 No hidden scope. Always visible.

---

### 3. Simple Override Model (MANDATORY)

We support:

- Global values
- Store-level overrides

We DO NOT support:
- deep inheritance chains
- website/store/view fallback trees

---

## 🗂 Admin Menu Structure

### Dashboard
- KPIs
- alerts
- context-aware metrics

---

### Sales
- Orders
- Returns
- Transactions

---

### Catalog
- Products
- Categories
- Attributes

---

### Customers
- Customers
- Groups

---

### Marketing
- Promotions
- Coupons

---

### Content
- Pages
- Navigation
- Blocks
- Media

---

### Operations
- Inventory
- Shipping
- Payments

---

### Settings

#### General
- store name
- email
- basic config

#### Localization
- currency formats
- tax settings

#### Users & Roles
- admin users
- permissions

---

### Store Management (separate from Settings)

- Stores
- Domains
- Languages
- Currencies

👉 This is STRUCTURE, not configuration.

---

### Integrations
- APIs
- webhooks
- external services

---

## 🧩 Scoped Data Model

Each field must define its scope:

| Field Type        | Scope        |
|------------------|-------------|
| SKU              | Global      |
| Name             | Translatable|
| Description      | Translatable|
| Price            | Per-store   |
| Status           | Per-store   |

---

## 🧑‍💻 UI Patterns for Scoped Data

### Product Editing

[ Global | Store: EU | Store: US ]

Name: [ translatable ]
Price: [ store-specific ]
SKU: [ global ]

---

### Content Editing


Page: Homepage

Languages:
[ EN | PL | DE ]

Stores:
[ EU ✔ | US ✖ ]


---

### Configuration Editing

Instead of hidden scopes:


Editing: Tax Settings

Scope:
[ Global ▼ | EU ▼ ]


---

## ⚠️ Critical Rules

### 1. No "Configuration Graveyard"

DO NOT create:
- giant config trees
- deeply nested settings

👉 Settings belong to features when possible.

---

### 2. Max 3 Levels of Navigation

- Top level
- Section
- Page

If more is needed → structure is wrong.

---

### 3. Co-locate Configuration with Features

Example:

❌ Wrong:
- Settings → Payments → Stripe

✅ Right:
- Operations → Payments → Stripe

---

### 4. Prefer Visibility Over Cleverness

❌ Hidden inheritance  
❌ Implicit overrides  

✅ Explicit scope  
✅ Clear UI indicators  

---

### 5. Avoid Splitting UI by Store

❌ Separate menus per store  
❌ Duplicate sections  

✅ One UI + context switcher  

---

## 🔧 Example: Payment Configuration


Operations → Payments → Stripe

Enabled for:
[ EU ✔ ]
[ US ✔ ]

Currencies:
[ EUR, PLN ]

Settings:

API Key
Webhook URL

---

## 🧱 Structural vs Behavioral Separation

### Structural (Store Management)
- stores
- domains
- languages

### Behavioral (Feature Config)
- shipping rules
- payment settings
- tax rules

👉 Never mix these.

---

## 🚀 Summary

We are building:

- a **context-driven admin**
- with **explicit scoping**
- using a **simple override model**
- organized by **business domains**
- with **configuration close to features**

---

## ❌ What We Explicitly Avoid

- Magento-style config trees
- hidden scope inheritance
- over-normalized UI
- menu structures based on database tables

---

## ✅ What We Optimize For

- clarity
- predictability
- scalability without complexity
- fast onboarding for admin users

---

## 🔜 Next Steps

- Define backend data model for scoped attributes
- Implement context-aware API layer
- Design permission system aligned with domains
- Add audit trail for scoped changes
