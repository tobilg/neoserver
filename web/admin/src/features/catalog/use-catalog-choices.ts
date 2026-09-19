import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";
import { useListServices } from "@/api/generated/services/services";
import { getListLayersQueryOptions } from "@/api/generated/layers/layers";
import { getListCoveragesQueryOptions } from "@/api/generated/coverages/coverages";
import { useListLayerGroups } from "@/api/generated/layer-groups/layer-groups";
import { useListStyles } from "@/api/generated/styles/styles";
import { useListWorkspaceRoles } from "@/api/generated/roles/roles";
import type {
  ListLayers200,
  CoverageList,
  Error as APIError,
} from "@/api/generated/models";
import type { UseQueryResult } from "@tanstack/react-query";
import type { FieldChoices } from "@/lib/schema-fields";
import { datasourceCapabilities } from "@/lib/datasource-capabilities";

const empty: never[] = [];
const combineLayers = (queries: UseQueryResult<ListLayers200, APIError>[]) => ({
  publications: queries.flatMap((query) => query.data?.layers ?? []),
  error: queries.find((query) => query.error)?.error,
  isLoading: queries.some((query) => query.isLoading),
  retry: () => Promise.all(queries.map((query) => query.refetch())),
});

const combineCoverages = (
  queries: UseQueryResult<CoverageList, APIError>[],
) => ({
  coverages: queries.flatMap((query) => query.data?.coverages ?? []),
  error: queries.find((query) => query.error)?.error,
  isLoading: queries.some((query) => query.isLoading),
  retry: () => Promise.all(queries.map((query) => query.refetch())),
});

export function useCatalogChoices(workspace: string, includeCoverages = true) {
  const services = useListServices(workspace);
  const layers = useQueries({
    queries: (services.data?.services ?? [])
      .filter((service) => datasourceCapabilities(service.type).features)
      .map((service) => getListLayersQueryOptions(workspace, service.id)),
    combine: combineLayers,
  });
  const groups = useListLayerGroups(workspace);
  const coverageQueries = useQueries({
    queries: (services.data?.services ?? [])
      .filter(
        (service) =>
          includeCoverages && datasourceCapabilities(service.type).coverages,
      )
      .map((service) => getListCoveragesQueryOptions(workspace, service.id)),
    combine: combineCoverages,
  });
  const coverages = coverageQueries.coverages;
  const styles = useListStyles(workspace);
  const roles = useListWorkspaceRoles(workspace);
  // useQueries structurally shares combined results even as query status changes.
  const publications = layers.publications;
  const groupData = groups.data?.layer_groups ?? empty;
  const allResources = useMemo(
    () => [
      ...publications.map((item) => ({
        ...item,
        kind: "feature" as const,
        storeEnabled:
          services.data?.services?.find((store) => store.id === item.service_id)
            ?.enabled !== false,
      })),
      ...coverages.map((item) => ({
        ...item,
        kind: "coverage" as const,
        storeEnabled:
          services.data?.services?.find((store) => store.id === item.service_id)
            ?.enabled !== false,
      })),
      ...groupData.map((item) => ({
        ...item,
        kind: "group" as const,
        storeEnabled: true,
      })),
    ],
    [publications, coverages, groupData, services.data],
  );
  const resources = useMemo(
    () =>
      allResources.filter(
        (item) => item.enabled !== false && item.storeEnabled,
      ),
    [allResources],
  );
  const choices = useMemo<FieldChoices>(() => {
    const styleChoices = (styles.data?.styles ?? []).map((style) => ({
      value: style.name,
      label:
        style.title && style.title !== style.name
          ? `${style.title} (${style.name})`
          : style.name,
    }));
    const roleChoices = (roles.data?.roles ?? []).map((role) => ({
      value: role.id,
      label: role.name || role.id,
    }));
    return {
      default_style: styleChoices,
      style: styleChoices,
      styles: styleChoices,
      allowed_roles: roleChoices,
      role_id: roleChoices,
      resource: resources.map((item) => ({
        value: item.public_id,
        label: `${item.title || item.public_id} (${item.public_id}, ${item.kind})`,
      })),
    };
  }, [resources, styles.data, roles.data]);
  return {
    choices,
    resources,
    allResources,
    publications,
    coverages,
    services: services.data?.services ?? empty,
    groups: groupData,
    isLoading:
      services.isLoading ||
      coverageQueries.isLoading ||
      layers.isLoading ||
      groups.isLoading ||
      styles.isLoading ||
      roles.isLoading,
    error:
      services.error ||
      coverageQueries.error ||
      styles.error ||
      roles.error ||
      groups.error ||
      layers.error,
    retry: () =>
      Promise.all([
        services.refetch(),
        styles.refetch(),
        roles.refetch(),
        groups.refetch(),
        layers.retry(),
        coverageQueries.retry(),
      ]),
  };
}
