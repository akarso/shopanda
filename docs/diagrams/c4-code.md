# C4 Level 4 — Code Diagram

Shows the domain model entities, their relationships, and the hexagonal port/adapter boundaries.

## Domain Entities & Relationships

```mermaid
classDiagram
    direction TB

    class Product {
        +string ID
        +string Name
        +string Slug
        +string Status
        +map Attributes
        +time CreatedAt
        +time UpdatedAt
    }

    class Variant {
        +string ID
        +string ProductID
        +string SKU
        +string Name
        +map Attributes
        +time CreatedAt
        +time UpdatedAt
    }

    class Price {
        +string ID
        +string VariantID
        +Money Amount
    }

    class Money {
        +int64 Amount
        +string Currency
    }

    class Category {
        +string ID
        +string Name
        +string Slug
        +string ParentID
        +string Description
        +map Meta
        +int Position
    }

    class Collection {
        +string ID
        +string Name
        +string Slug
        +CollectionType Type
        +map Rules
        +map Meta
        +IsManual() bool
        +IsDynamic() bool
    }

    class Cart {
        +string ID
        +string CustomerID
        +string Status
        +string Currency
        +[]Item Items
    }

    class CartItem {
        +string VariantID
        +int Quantity
        +Money UnitPrice
    }

    class Order {
        +string ID
        +string CustomerID
        +string Status
        +Money TotalAmount
        +[]OrderItem Items
    }

    class OrderItem {
        +string VariantID
        +string SKU
        +string Name
        +int Quantity
        +Money UnitPrice
    }

    class Customer {
        +string ID
        +string Email
        +string FirstName
        +string LastName
        +string Role
        +string Status
    }

    class Stock {
        +string VariantID
        +int Quantity
    }

    class Reservation {
        +string ID
        +string VariantID
        +string OrderID
        +int Quantity
        +time ExpiresAt
    }

    class Payment {
        +string ID
        +string OrderID
        +string Method
        +string Status
        +Money Amount
    }

    class Shipment {
        +string ID
        +string OrderID
        +string Method
        +string Status
        +Money Cost
        +string TrackingNumber
    }

    class Identity {
        +string CustomerID
        +string Role
    }

    class SearchQuery {
        +string Text
        +map Filters
        +string Sort
        +int Limit
        +int Offset
        +Validate() error
        +EffectiveLimit() int
    }

    class SearchResult {
        +[]SearchProduct Products
        +int Total
        +map~string, []FacetValue~ Facets
    }

    class FacetValue {
        +string Value
        +int Count
    }

    class SearchProduct {
        +string ID
        +string Name
        +string Slug
        +string Description
        +map Attributes
    }

    class Job {
        +string ID
        +string Type
        +map Payload
        +Status Status
        +int Attempts
        +int MaxRetries
        +time RunAt
        +time CreatedAt
        +time UpdatedAt
    }

    class Status {
        <<enumeration>>
        pending
        processing
        done
        failed
    }

    Job --> Status : has

    Product "1" --> "*" Variant : has
    Variant "1" --> "1" Price : priced by
    Variant "1" --> "1" Stock : tracked by
    Variant "1" --> "*" Reservation : reserved in
    Product "*" --> "*" Category : categorized in
    Product "*" --> "*" Collection : grouped in
    Category "0..1" --> "*" Category : parent of
    SearchResult --> SearchProduct : contains
    SearchResult --> FacetValue : contains
    Price --> Money : uses
    Cart "1" --> "*" CartItem : contains
    CartItem --> Variant : references
    Order "1" --> "*" OrderItem : contains
    Order "1" --> "0..1" Payment : paid via
    Order "1" --> "0..1" Shipment : shipped via
    Customer "1" --> "*" Cart : owns
    Customer "1" --> "*" Order : places
    Cart --> Order : checked out as
```

## Hexagonal Architecture — Ports & Adapters

```mermaid
classDiagram
    direction LR

    class ProductRepository {
        <<interface>>
        +FindByID(ctx, id) Product
        +FindBySlug(ctx, slug) Product
        +List(ctx, offset, limit) []Product
        +FindByCategoryID(ctx, catID, offset, limit) []Product
        +Create(ctx, product) error
        +Update(ctx, product) error
        +WithTx(tx) ProductRepository
    }

    class CartRepository {
        <<interface>>
        +FindByID(ctx, id) Cart
        +FindActiveByCustomerID(ctx, custID) Cart
        +Save(ctx, cart) error
        +Delete(ctx, id) error
    }

    class OrderRepository {
        <<interface>>
        +FindByID(ctx, id) Order
        +FindByCustomerID(ctx, customerID) []Order
        +List(ctx, offset, limit) []Order
        +Save(ctx, order) error
        +UpdateStatus(ctx, order) error
    }

    class CategoryRepository {
        <<interface>>
        +FindByID(ctx, id) Category
        +FindBySlug(ctx, slug) Category
        +FindAll(ctx) []Category
        +Create(ctx, cat) error
        +Update(ctx, cat) error
    }

    class CollectionRepository {
        <<interface>>
        +FindByID(ctx, id) Collection
        +FindBySlug(ctx, slug) Collection
        +List(ctx, offset, limit) []Collection
        +Create(ctx, coll) error
        +Update(ctx, coll) error
        +AddProduct(ctx, collID, productID) error
        +RemoveProduct(ctx, collID, productID) error
        +ListProductIDs(ctx, collID) []string
    }

    class PricingStep {
        <<interface>>
        +Name() string
        +Apply(ctx, *PricingContext) error
    }

    class Step {
        <<interface>>
        +Name() string
        +Execute(ctx *Context) error
    }

    class Plugin {
        <<interface>>
        +Name() string
        +Init(app) error
    }

    class SearchEngine {
        <<interface>>
        +Name() string
        +IndexProduct(ctx, product) error
        +RemoveProduct(ctx, productID) error
        +Search(ctx, query) ~SearchResult, error~
    }

    class Queue {
        <<interface>>
        +Enqueue(ctx, job) error
        +Dequeue(ctx) ~*Job, error~
        +Complete(ctx, jobID) error
        +Fail(ctx, jobID, jobErr) error
    }

    class Handler {
        <<interface>>
        +Type() string
        +Handle(ctx, job) error
    }

    class Worker {
        -queue Queue
        -handlers map
        -log Logger
        -pollInterval Duration
        +Register(h Handler)
        +Start(ctx)
        +Stop()
    }

    class Scheduler {
        <<interface>>
        +Register(name, spec, fn)
        +Start(ctx)
        +Stop()
    }

    class Mailer {
        <<interface>>
        +Send(ctx, msg) error
    }

    class Message {
        +string To
        +string Subject
        +string Body
    }

    class Templates {
        -tmpls map
        +Register(name, subject, body)
        +Render(name, to, data) (Message, error)
    }

    class PostgresProductRepo {
        -db *sql.DB
    }
    class PostgresCartRepo {
        -db *sql.DB
    }
    class PostgresOrderRepo {
        -db *sql.DB
    }
    class PostgresCategoryRepo {
        -db *sql.DB
    }
    class PostgresCollectionRepo {
        -db *sql.DB
    }
    class ManualPayProvider {
    }
    class FlatRateShipProvider {
        -cost Money
    }
    class BasePriceStep {
        -prices PriceRepository
    }
    class PostgresSearchEngine {
        -db *sql.DB
    }
    class JobQueue {
        -db *sql.DB
    }
    class CronScheduler {
        -entries []entry
        -log Logger
    }
    class SMTPMailer {
        -cfg Config
    }
    class Asset {
        +string ID
        +string Path
        +string Filename
        +string MimeType
        +int64 Size
        +map Meta
        +time CreatedAt
    }
    class Storage {
        <<interface>>
        +Name() string
        +Save(path, file) error
        +Delete(path) error
        +URL(path) string
    }
    class LocalStorage {
        -basePath string
        -baseURL string
    }
    class AssetRepository {
        <<interface>>
        +Save(ctx, asset) error
        +FindByID(ctx, id) Asset
    }
    class PostgresAssetRepo {
        -db *sql.DB
    }
    class Cache {
        <<interface>>
        +Get(key, dest) ~bool, error~
        +Set(key, value, ttl) error
        +Delete(key) error
    }
    class PostgresCacheStore {
        -db *sql.DB
        +DeleteExpired() ~int64, error~
    }
    class ConfigRepository {
        <<interface>>
        +Get(ctx, key) ~interface{}, error~
        +Set(ctx, key, value) error
        +Delete(ctx, key) error
        +All(ctx) ~[]Entry, error~
    }
    class PostgresConfigRepo {
        -db *sql.DB
    }
    class NotificationService {
        -templates Templates
        -customers CustomerRepository
        -orders OrderRepository
        -queue Queue
        +HandleOrderPaid(ctx, evt) error
    }
    class EmailSendHandler {
        -mailer Mailer
        +Type() string
        +Handle(ctx, job) error
    }
    class CacheCleanupHandler {
        -deleter ExpiredDeleter
        -log Logger
        +Type() string
        +Handle(ctx, job) error
    }
    class AdminRegistry {
        -forms map~string, *Form~
        -grids map~string, *Grid~
        +RegisterForm(name, form)
        +RegisterFormField(formName, field) error
        +RegisterGrid(name, grid)
        +RegisterGridColumn(gridName, column) error
        +RegisterAction(gridName, action) error
        +Form(name) ~Form, bool~
        +Grid(name) ~Grid, bool~
    }
    class Form {
        +string Name
        +[]Field Fields
    }
    class Field {
        +string Name
        +string Type
        +string Label
        +bool Required
        +interface{} Default
        +[]Option Options
        +map Meta
    }
    class Grid {
        +string Name
        +[]Column Columns
        +[]Action Actions
    }
    class Column {
        +string Name
        +string Label
        +func Value
        +map Meta
    }
    class Action {
        +string Name
        +string Label
        +func Execute
    }
    class SearchHandler {
        -engine SearchEngine
        +Search() HandlerFunc
    }
    class SchemaHandler {
        -registry *AdminRegistry
        +GetForm() HandlerFunc
        +GetGrid() HandlerFunc
    }
    class ThemeEngine {
        -theme Theme
        -pages map~string, *Template~
        +Load(dir) ~*Engine, error~
        +Theme() Theme
        +Render(w, name, data) error
        +HasTemplate(name) bool
    }
    class Theme {
        +string Name
        +string Version
    }

    ProductRepository <|.. PostgresProductRepo : implements
    CartRepository <|.. PostgresCartRepo : implements
    OrderRepository <|.. PostgresOrderRepo : implements
    CategoryRepository <|.. PostgresCategoryRepo : implements
    CollectionRepository <|.. PostgresCollectionRepo : implements
    SearchEngine <|.. PostgresSearchEngine : implements
    Queue <|.. JobQueue : implements
    Scheduler <|.. CronScheduler : implements
    Mailer <|.. SMTPMailer : implements
    Storage <|.. LocalStorage : implements
    AssetRepository <|.. PostgresAssetRepo : implements
    Cache <|.. PostgresCacheStore : implements
    ConfigRepository <|.. PostgresConfigRepo : implements
    Templates --> Message : produces
    PricingStep <|.. BasePriceStep : implements
    SearchHandler --> SearchEngine : uses
    SchemaHandler --> AdminRegistry : reads schemas
    ThemeEngine --> Theme : holds metadata
    Worker --> Queue : polls
    Worker --> Handler : dispatches to
    Handler <|.. EmailSendHandler : implements
    Handler <|.. CacheCleanupHandler : implements
    CacheCleanupHandler --> PostgresCacheStore : calls DeleteExpired
    AdminRegistry --> Form : manages
    AdminRegistry --> Grid : manages
    Form --> Field : contains
    Grid --> Column : contains
    Grid --> Action : contains
    EmailSendHandler --> Mailer : sends via
    NotificationService --> Templates : renders
    NotificationService --> Queue : enqueues email.send
```
