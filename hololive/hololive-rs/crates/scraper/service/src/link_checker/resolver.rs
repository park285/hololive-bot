use std::net::IpAddr;

use hickory_resolver::{
    TokioResolver,
    config::{ResolverConfig, ResolverOpts},
    name_server::TokioConnectionProvider,
};

use crate::repository::BoxFuture;

pub(super) trait HostResolver: Send + Sync {
    fn resolve(&self, host: &str, _port: u16) -> BoxFuture<'_, Result<Vec<IpAddr>, String>>;
}

#[derive(Clone)]
pub(super) struct TokioHostResolver {
    resolver: TokioResolver,
}

impl Default for TokioHostResolver {
    fn default() -> Self {
        let resolver = TokioResolver::builder_with_config(
            ResolverConfig::default(),
            TokioConnectionProvider::default(),
        )
        .with_options(ResolverOpts::default())
        .build();
        Self { resolver }
    }
}

impl HostResolver for TokioHostResolver {
    fn resolve(&self, host: &str, _port: u16) -> BoxFuture<'_, Result<Vec<IpAddr>, String>> {
        let target_host = host.to_owned();
        let resolver = self.resolver.clone();

        Box::pin(async move {
            let result = resolver
                .lookup_ip(target_host.as_str())
                .await
                .map_err(|err| err.to_string())?;

            let mut ips: Vec<IpAddr> = Vec::new();
            for ip in result.iter() {
                ips.push(ip);
            }

            Ok(ips)
        })
    }
}
