output "address" {
    value = "${aws_elb.vault.dns_name}"
}

output "zone_id" {
    value = "${aws_elb.vault.zone_id}"
}


// Can be used to add additional SG rules to Vault instances.
output "vault_security_group" {
    value = "${aws_security_group.vault.id}"
}

// Can be used to add additional SG rules to the Vault ELB.
output "elb_security_group" {
    value = "${aws_security_group.elb.id}"
}
