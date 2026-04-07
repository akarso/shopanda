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

    Product "1" --> "*" Variant : has
    Variant "1" --> "1" Price : priced by
    Variant "1" --> "1" Stock : tracked by
    Variant "1" --> "*" Reservation : reserved in
    Product "*" --> "*" Category : categorized in
    Product "*" --> "*" Collection : grouped in
    Category "0..1" --> "*" Category : parent of
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

    ProductRepository <|.. PostgresProductRepo : implements
    CartRepository <|.. PostgresCartRepo : implements
    OrderRepository <|.. PostgresOrderRepo : implements
    CategoryRepository <|.. PostgresCategoryRepo : implements
    CollectionRepository <|.. PostgresCollectionRepo : implements
    PricingStep <|.. BasePriceStep : implements
```
