package store

// ResolveInvoiceAddress returns the invoice address when it has postal content,
// otherwise general. Missing or empty roles yield nil.
func ResolveInvoiceAddress(invoice, general *Address) *Address {
	if invoice != nil && !invoice.IsEmpty() {
		return invoice
	}
	if general != nil && !general.IsEmpty() {
		return general
	}
	return nil
}

// ResolveReturnsAddress returns returns → shipping_origin → general (postal content).
func ResolveReturnsAddress(returnsAddr, shippingOrigin, general *Address) *Address {
	if returnsAddr != nil && !returnsAddr.IsEmpty() {
		return returnsAddr
	}
	if shippingOrigin != nil && !shippingOrigin.IsEmpty() {
		return shippingOrigin
	}
	if general != nil && !general.IsEmpty() {
		return general
	}
	return nil
}
