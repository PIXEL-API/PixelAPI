import defaultMdxComponents from 'fumadocs-ui/mdx';
import type { MDXComponents } from 'mdx/types';
import { Accordion, Accordions } from 'fumadocs-ui/components/accordion';
import { Callout } from 'fumadocs-ui/components/callout';
import { Card, Cards } from 'fumadocs-ui/components/card';
import { Step, Steps } from 'fumadocs-ui/components/steps';
import { Tab, Tabs } from 'fumadocs-ui/components/tabs';
import {
  ApiExample,
  EndpointReference,
  ParameterRow,
  ParameterTable,
} from '@/components/api-reference';
import { ModelApiReference } from '@/components/model-api-reference';
import { QqGroups } from '@/components/qq-groups';
import { Screenshot } from '@/components/screenshot';

export function getMDXComponents(components?: MDXComponents): MDXComponents {
  return {
    ...defaultMdxComponents,
    Accordion,
    Accordions,
    ApiExample,
    Callout,
    Card,
    Cards,
    EndpointReference,
    ModelApiReference,
    ParameterRow,
    ParameterTable,
    QqGroups,
    Screenshot,
    Step,
    Steps,
    Tab,
    Tabs,
    ...components,
  };
}
